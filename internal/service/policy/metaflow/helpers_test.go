package metaflow_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

func ptr[T any](v T) *T { return &v }

func testThresholds() policy.Thresholds {
	side := config.PolicyDirectionThresholds{
		MinProbability: 0.68, MinEntryQuality: "strong",
		MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
	}
	return policy.Thresholds{Policy: config.PolicyConfig{Long: side, Short: side}, MaxSpreadBps: 50, MinTurnover5mJPY: 3_000_000}
}

// passingInput returns an input for a Jev Trader decision that clears every
// guard at the boundary values (confidence 0.68, toxic_flow 0.35, ...), with
// the meta-labeling model's inputs filled in.
func passingInput(direction string) policy.Input {
	return policytest.WithInputs(policy.Input{
		InstrumentID: 1, Symbol: "7203", Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
		Decision: &domain.JevDecision{
			ID: 7, Direction: ptr(direction), EntryQuality: ptr(domain.JevEntryQualityStrong), Confidence: ptr(0.68),
			ContinuationProbability: ptr(0.60), ToxicFlow: ptr(0.35), LiquidityStressed: ptr(0.25),
		},
		SpreadBps: ptr(10.0), EntryPriceReference: ptr(2110.5), Calibrated: true,
	})
}

type fakeRiskChecker struct {
	passed bool
	reason string
}

func (f fakeRiskChecker) Check(context.Context, int64, string) (bool, string) {
	return f.passed, f.reason
}

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// captureLogs routes slog to a buffer as JSON for the test.
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
