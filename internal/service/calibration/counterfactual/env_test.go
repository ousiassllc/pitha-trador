package counterfactual_test

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	calrepo "github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/counterlabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration/counterfactual"
)

func ptr[T any](v T) *T { return &v }

type bar struct{ price, bid, ask float64 }

// tpBars is a LONG take-profit path (default barriers: TP 30 bps net, cost 10):
// the entry at ask 1000, +45 bps by the second minute, then the horizon bar.
var tpBars = map[time.Duration]bar{
	0:               {price: 999, bid: 998, ask: 1000},
	1 * time.Minute: {price: 1002, bid: 1001, ask: 1003},
	2 * time.Minute: {price: 1004.5, bid: 1004, ask: 1005},
	5 * time.Minute: {price: 1000, bid: 999, ask: 1001},
}

type env struct {
	db        *sql.DB
	instID    int64
	decisions *judgement.DecisionRepository
	snapshots *market.SnapshotRepository
	outcomes  *calrepo.CalibrationRepository
	labeler   *calibration.Labeler
	store     *counterlabel.Repository
	recorder  *counterfactual.Recorder
	settings  metalabelsettings.Settings
	now       time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	res, err := conn.Exec(`INSERT INTO instruments (symbol, name, market, is_active) VALUES ('7203', 'x', 'TSE Prime', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	instID, _ := res.LastInsertId()
	e := &env{db: conn, instID: instID, settings: metalabelsettings.Default()}
	e.decisions, e.snapshots, e.outcomes = judgement.NewDecisionRepository(conn), market.NewSnapshotRepository(conn), calrepo.NewCalibrationRepository(conn)
	e.labeler = calibration.NewLabeler(e.decisions, e.snapshots, e.outcomes)
	e.store = counterlabel.NewRepository(conn)
	e.settings.LLMCutoff, e.settings.LLMCutoffModel = "2026-09-20", "m"
	e.now = time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC) // two days after the decisions
	e.recorder = counterfactual.New(e.store, e.labeler, e.outcomes,
		func(context.Context) (metalabelsettings.Settings, error) { return e.settings, nil },
		func(context.Context) ([]int, error) { return []int{5}, nil },
	).WithClock(func() time.Time { return e.now })
	return e
}

func (e *env) decision(t *testing.T, ts time.Time, direction string) domain.JevDecision {
	t.Helper()
	d, err := e.decisions.Insert(context.Background(), domain.JevDecision{
		InstrumentID: e.instID, Symbol: "7203", Timestamp: ts, DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "h", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", Direction: ptr(direction),
		Confidence: ptr(0.8), ModelID: "m", CreatedAt: ts,
	})
	if err != nil {
		t.Fatalf("insert decision: %v", err)
	}
	return d
}

func (e *env) bars(t *testing.T, base time.Time, bars map[time.Duration]bar) {
	t.Helper()
	var snaps []domain.Snapshot
	for offset, b := range bars {
		s := domain.Snapshot{InstrumentID: e.instID, Symbol: "7203", Timestamp: base.Add(offset), Price: b.price}
		if b.bid > 0 {
			s.Bid = ptr(b.bid)
		}
		if b.ask > 0 {
			s.Ask = ptr(b.ask)
		}
		snaps = append(snaps, s)
	}
	if _, err := e.snapshots.InsertBatch(context.Background(), snaps); err != nil {
		t.Fatalf("insert snapshots: %v", err)
	}
}

func (e *env) signal(t *testing.T, decisionID int64, ts time.Time, riskPassed bool, reason string) {
	t.Helper()
	if _, err := e.db.Exec(`INSERT INTO trade_signals (instrument_id, jev_decision_id, symbol, timestamp, direction, policy_version, risk_passed, reject_reason)
		VALUES (?, ?, '7203', ?, 'NONE', 'policy-v1', ?, NULLIF(?, ''))`, e.instID, decisionID, ts.UTC().Format(time.RFC3339Nano), riskPassed, reason); err != nil {
		t.Fatalf("insert trade signal: %v", err)
	}
}

func (e *env) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
