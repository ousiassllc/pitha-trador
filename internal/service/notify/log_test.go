package notify_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/notify"
)

// newBufferLogger returns a slog.Logger whose JSON records land in buf.
func newBufferLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// decodeRecord parses the single JSON log line in buf.
func decodeRecord(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 1 || len(lines[0]) == 0 {
		t.Fatalf("log output = %q, want exactly one record", buf.String())
	}
	var rec map[string]any
	if err := json.Unmarshal(lines[0], &rec); err != nil {
		t.Fatalf("decode log record %q: %v", lines[0], err)
	}
	return rec
}

func TestLogNotifier_AlertsLogWarnWithKeyAttributes(t *testing.T) {
	ev := domain.KillSwitchEvent{
		Reason:      domain.KillReasonJevAPIDown,
		TriggeredAt: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC),
	}
	halt := domain.DailyEntryHalt{
		Day:     "2026-09-28",
		Reason:  domain.DailyHaltReasonLossLimit,
		Message: "loss limit reached",
	}
	cause := errors.New("anthropic api timeout")

	tests := []struct {
		name  string
		call  func(n *notify.LogNotifier) error
		msg   string
		attrs map[string]any
	}{
		{
			name: "KillSwitchTriggered",
			call: func(n *notify.LogNotifier) error {
				return n.KillSwitchTriggered(context.Background(), ev, true)
			},
			msg:   "alert: kill switch triggered",
			attrs: map[string]any{"reason": "jev_api_down", "reason_label": "Jev API連続失敗", "auto_resumable": true},
		},
		{
			name: "KillSwitchAutoResumed",
			call: func(n *notify.LogNotifier) error {
				return n.KillSwitchAutoResumed(context.Background(), ev)
			},
			msg:   "alert: kill switch auto-resumed",
			attrs: map[string]any{"reason": "jev_api_down", "reason_label": "Jev API連続失敗"},
		},
		{
			name: "DailyLossWarning",
			call: func(n *notify.LogNotifier) error {
				return n.DailyLossWarning(context.Background(), 0.8, 1.0)
			},
			msg:   "alert: daily loss approaching limit",
			attrs: map[string]any{"current_pct": 0.8, "limit_pct": 1.0},
		},
		{
			name: "JevAPIErrorRateExceeded",
			call: func(n *notify.LogNotifier) error {
				return n.JevAPIErrorRateExceeded(context.Background(), 0.6, 0.5)
			},
			msg:   "alert: jev api error rate exceeded",
			attrs: map[string]any{"rate": 0.6, "threshold": 0.5},
		},
		{
			name: "ProposalApplied",
			call: func(n *notify.LogNotifier) error {
				return n.ProposalApplied(context.Background(), domain.PolicyProposal{ID: 42})
			},
			msg:   "alert: policy proposal applied",
			attrs: map[string]any{"proposal_id": float64(42)},
		},
		{
			name: "ProposalRolledBack",
			call: func(n *notify.LogNotifier) error {
				return n.ProposalRolledBack(context.Background(), domain.PolicyProposal{ID: 42}, "expectancy degraded 25%")
			},
			msg:   "alert: policy proposal rolled back",
			attrs: map[string]any{"proposal_id": float64(42), "reason": "expectancy degraded 25%"},
		},
		{
			name: "AIStageSkipped",
			call: func(n *notify.LogNotifier) error {
				return n.AIStageSkipped(context.Background(), "Sol提案生成", cause)
			},
			msg:   "alert: self-improvement AI stage skipped, retry next business day",
			attrs: map[string]any{"stage": "Sol提案生成", "error": "anthropic api timeout"},
		},
		{
			name: "DailyEntryHalted",
			call: func(n *notify.LogNotifier) error {
				return n.DailyEntryHalted(context.Background(), halt)
			},
			msg: "alert: daily entry halt, new entries stopped for the trading day",
			attrs: map[string]any{
				"reason": "daily_loss_limit_jpy", "reason_label": "1日の損失上限",
				"message": "loss limit reached", "day": "2026-09-28",
			},
		},
		{
			name: "MaintenanceFailed",
			call: func(n *notify.LogNotifier) error {
				return n.MaintenanceFailed(context.Background(), "DBバックアップ", 3, errors.New("drive not mounted"))
			},
			msg:   "alert: maintenance task failing repeatedly",
			attrs: map[string]any{"task": "DBバックアップ", "consecutive_failures": float64(3), "error": "drive not mounted"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			n := notify.NewLogNotifier(newBufferLogger(&buf))

			if err := tt.call(n); err != nil {
				t.Fatalf("%s returned %v, want nil", tt.name, err)
			}
			rec := decodeRecord(t, &buf)
			if rec["level"] != "WARN" {
				t.Errorf("level = %v, want WARN", rec["level"])
			}
			if rec["msg"] != tt.msg {
				t.Errorf("msg = %v, want %q", rec["msg"], tt.msg)
			}
			for k, want := range tt.attrs {
				if got := rec[k]; got != want {
					t.Errorf("attr %q = %v (%T), want %v (%T)", k, got, got, want, want)
				}
			}
		})
	}
}

func TestLogNotifier_KillSwitchTriggered_UnknownReasonFallsBackToRawReason(t *testing.T) {
	var buf bytes.Buffer
	n := notify.NewLogNotifier(newBufferLogger(&buf))

	if err := n.KillSwitchTriggered(context.Background(), domain.KillSwitchEvent{Reason: "brand_new_reason"}, false); err != nil {
		t.Fatalf("KillSwitchTriggered: %v", err)
	}
	rec := decodeRecord(t, &buf)
	if rec["reason_label"] != "brand_new_reason" || rec["auto_resumable"] != false {
		t.Errorf("record = %v, want raw reason as label and auto_resumable=false", rec)
	}
}

// TestNewLogNotifier_NilLoggerUsesSlogDefault mutates the process-wide
// slog default, so it must not run in parallel.
func TestNewLogNotifier_NilLoggerUsesSlogDefault(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(newBufferLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(prev) })

	n := notify.NewLogNotifier(nil)
	if err := n.DailyLossWarning(context.Background(), 0.8, 1.0); err != nil {
		t.Fatalf("DailyLossWarning: %v", err)
	}
	if rec := decodeRecord(t, &buf); rec["msg"] != "alert: daily loss approaching limit" {
		t.Errorf("msg = %v, want it written through slog.Default()", rec["msg"])
	}
}
