package policy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func decodeLogLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatalf("log line is not valid JSON: %v\nline: %s", err, raw)
		}
		lines = append(lines, decoded)
	}
	return lines
}

func TestEngine_Evaluate_LogsTradeSignalDecidedOncePerEvaluation(t *testing.T) {
	db := newHandlerTestDB(t)
	instrument, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create instrument: %v", err)
	}
	in := passingInput(domain.JevDirectionLong)
	in.InstrumentID = instrument.ID
	saved, err := judgement.NewDecisionRepository(db).Insert(context.Background(), domain.JevDecision{
		InstrumentID: instrument.ID, Symbol: "7203", Timestamp: in.Timestamp, DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "hash", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "test-model",
	})
	if err != nil {
		t.Fatalf("Insert decision: %v", err)
	}
	in.Decision.ID = saved.ID
	buf := captureLogs(t)
	e := policy.NewEngine(testThresholds(), fakeRiskChecker{passed: true}, trading.NewSignalRepository(db))

	if _, err := e.Evaluate(context.Background(), in); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	count := 0
	for _, line := range decodeLogLines(t, buf) {
		if line["msg"] == "policy: trade signal decided" {
			count++
			if line["direction"] != domain.JevDirectionLong {
				t.Errorf("direction = %v, want %q", line["direction"], domain.JevDirectionLong)
			}
			if line["risk_passed"] != true {
				t.Errorf("risk_passed = %v, want true", line["risk_passed"])
			}
		}
	}
	if count != 1 {
		t.Fatalf("'policy: trade signal decided' lines = %d, want 1; lines: %+v", count, decodeLogLines(t, buf))
	}
}

// Decide is the side-effect-free path backtest replay calls on every
// bar; it must not inflate the §5.1 Signal count.
func TestEngine_Decide_DoesNotLogTradeSignalDecided(t *testing.T) {
	buf := captureLogs(t)
	e := policy.NewEngine(testThresholds(), fakeRiskChecker{passed: true}, nil)

	e.Decide(context.Background(), passingInput(domain.JevDirectionLong))

	for _, line := range decodeLogLines(t, buf) {
		if line["msg"] == "policy: trade signal decided" {
			t.Fatalf("Decide must not emit the Signal count log line: %+v", line)
		}
	}
}

func TestEngine_Decide_LogsRiskEngineRejection(t *testing.T) {
	buf := captureLogs(t)
	e := policy.NewEngine(testThresholds(), fakeRiskChecker{passed: false, reason: "max_open_positions exceeded"}, nil)

	e.Decide(context.Background(), passingInput(domain.JevDirectionLong))

	found := false
	for _, line := range decodeLogLines(t, buf) {
		if line["msg"] == "policy: risk engine rejected signal" {
			found = true
			if line["reason"] != "max_open_positions exceeded" {
				t.Errorf("reason = %v, want %q", line["reason"], "max_open_positions exceeded")
			}
		}
	}
	if !found {
		t.Fatalf("no 'policy: risk engine rejected signal' log line; lines: %+v", decodeLogLines(t, buf))
	}
}

func TestEngine_Decide_DoesNotLogRiskRejectionWhenAlreadyNone(t *testing.T) {
	buf := captureLogs(t)
	e := policy.NewEngine(testThresholds(), fakeRiskChecker{passed: false, reason: "should not be called"}, nil)

	in := passingInput(domain.JevDirectionLong)
	in.Decision.Direction = ptr(domain.JevDirectionNone)
	e.Decide(context.Background(), in)

	for _, line := range decodeLogLines(t, buf) {
		if line["msg"] == "policy: risk engine rejected signal" {
			t.Fatalf("risk rejection should not be logged when Risk Engine was never consulted: %+v", line)
		}
	}
}
