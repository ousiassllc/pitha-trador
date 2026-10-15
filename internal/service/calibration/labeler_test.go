package calibration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	calrepo "github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
)

func ptr[T any](v T) *T { return &v }

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// labelerFixtures wires a Labeler and its backing repositories against a
// single fresh test database, with one instrument already created.
type labelerFixtures struct {
	db         *sql.DB
	labeler    *calibration.Labeler
	decisions  *judgement.DecisionRepository
	snapshots  *market.SnapshotRepository
	outcomes   *calrepo.CalibrationRepository
	instrument domain.Instrument
	// now is the Labeler's clock; the zero value is before every
	// decision's data grace period, i.e. "data may still be landing".
	now *time.Time
}

func newLabelerFixtures(t *testing.T) labelerFixtures {
	t.Helper()
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}

	decisions := judgement.NewDecisionRepository(db)
	snapshots := market.NewSnapshotRepository(db)
	outcomes := calrepo.NewCalibrationRepository(db)
	now := new(time.Time)
	return labelerFixtures{
		db:         db,
		now:        now,
		labeler:    calibration.NewLabeler(decisions, snapshots, outcomes, calibration.WithClock(func() time.Time { return *now })),
		decisions:  decisions,
		snapshots:  snapshots,
		outcomes:   outcomes,
		instrument: inst,
	}
}

func (f labelerFixtures) insertDecision(t *testing.T, timestamp time.Time, direction string, confidence float64) domain.JevDecision {
	t.Helper()
	saved, err := f.decisions.Insert(context.Background(), domain.JevDecision{
		InstrumentID: f.instrument.ID, Symbol: f.instrument.Symbol, Timestamp: timestamp,
		DecisionType: domain.JevDecisionTypeTrader, StateHash: "hash", StateJSON: "{}",
		QuestionVersion: "v1", ResponseJSON: "{}", Direction: ptr(direction),
		Confidence: ptr(confidence), ModelID: "test-model",
		// The Jev response lands right at the decision's bar.
		CreatedAt: timestamp,
	})
	if err != nil {
		t.Fatalf("insert trader decision: %v", err)
	}
	return saved
}

func (f labelerFixtures) insertSnapshots(t *testing.T, prices map[time.Duration]float64, base time.Time) {
	t.Helper()
	var snaps []domain.Snapshot
	for offset, price := range prices {
		snaps = append(snaps, domain.Snapshot{
			InstrumentID: f.instrument.ID, Symbol: f.instrument.Symbol,
			Timestamp: base.Add(offset), Price: price,
			// A zero-spread quote at the last price, for tests that only
			// need the bars to exist.
			Bid: ptr(price), Ask: ptr(price),
		})
	}
	if _, err := f.snapshots.InsertBatch(context.Background(), snaps); err != nil {
		t.Fatalf("insert snapshots: %v", err)
	}
}

func (f labelerFixtures) handleJob(t *testing.T, decisionID int64, horizonMinutes int) error {
	t.Helper()
	payload, err := json.Marshal(calrepo.OutcomeLabelJobPayload{JevDecisionID: decisionID, HorizonMinutes: horizonMinutes})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return f.labeler.HandleJob(context.Background(), jobqueue.Job{PayloadJSON: string(payload)})
}

// bar is one market_snapshots row of a labeler test: the last price plus
// the executable bid/ask quotes.
type bar struct {
	price, bid, ask float64
}

// insertBars writes bars at base+offset. A zero bid/ask is stored as NULL
// (no quote).
func (f labelerFixtures) insertBars(t *testing.T, base time.Time, bars map[time.Duration]bar) {
	t.Helper()
	var snaps []domain.Snapshot
	for offset, b := range bars {
		s := domain.Snapshot{
			InstrumentID: f.instrument.ID, Symbol: f.instrument.Symbol,
			Timestamp: base.Add(offset), Price: b.price,
		}
		if b.bid > 0 {
			s.Bid = ptr(b.bid)
		}
		if b.ask > 0 {
			s.Ask = ptr(b.ask)
		}
		snaps = append(snaps, s)
	}
	if _, err := f.snapshots.InsertBatch(context.Background(), snaps); err != nil {
		t.Fatalf("insert snapshots: %v", err)
	}
}

// storedOutcome is the triple-barrier part of one calibration_outcomes row.
type storedOutcome struct {
	barrierHit    string
	netReturnBps  float64
	entryRefPrice float64
	labelVersion  int
	futureReturn  float64
	correct       sql.NullBool
}

func (f labelerFixtures) getTripleBarrier(t *testing.T, decisionID int64, horizonMinutes int) storedOutcome {
	t.Helper()
	var (
		o       storedOutcome
		hit     sql.NullString
		net     sql.NullFloat64
		entry   sql.NullFloat64
		version int
	)
	err := f.db.QueryRow(
		`SELECT barrier_hit, net_return_bps, entry_ref_price, label_version, future_return, was_direction_correct
		FROM calibration_outcomes WHERE jev_decision_id = ? AND horizon_minutes = ?`,
		decisionID, horizonMinutes,
	).Scan(&hit, &net, &entry, &version, &o.futureReturn, &o.correct)
	if err != nil {
		t.Fatalf("query calibration outcome for decision %d (horizon %dm): %v", decisionID, horizonMinutes, err)
	}
	o.barrierHit, o.netReturnBps, o.entryRefPrice, o.labelVersion = hit.String, net.Float64, entry.Float64, version
	return o
}

func TestLabeler_HandleJob_InsufficientDataIsDeferredNotFailed(t *testing.T) {
	f := newLabelerFixtures(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	decision := f.insertDecision(t, base, domain.JevDirectionLong, 0.82)
	// Only the entry bar exists; the 5-minute horizon bar hasn't landed yet.
	f.insertSnapshots(t, map[time.Duration]float64{0: 1000}, base)

	if err := f.handleJob(t, decision.ID, 5); !isDeferred(err) {
		t.Fatalf("HandleJob error = %v, want jobqueue.Defer (pending, not a failure) with no horizon-boundary snapshot yet", err)
	}
}

func TestLabeler_HandleJob_UnknownDecisionReturnsError(t *testing.T) {
	f := newLabelerFixtures(t)
	err := f.handleJob(t, 999999, 5)
	if err == nil || !errors.Is(err, judgement.ErrDecisionNotFound) {
		t.Fatalf("HandleJob(unknown decision) error = %v, want wrapping ErrDecisionNotFound", err)
	}
}

func almostEqual(a, b float64) bool {
	const epsilon = 1e-6
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
}
