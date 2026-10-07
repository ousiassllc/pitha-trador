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

// getOutcome reads one calibration_outcomes row directly (bypassing
// calrepo.CalibrationRepository, which intentionally has no
// query-by-decision method) for these tests to assert Labeler.HandleJob's
// computed values against.
func (f labelerFixtures) getOutcome(t *testing.T, decisionID int64, horizonMinutes int) domain.CalibrationOutcome {
	t.Helper()
	var (
		o       domain.CalibrationOutcome
		correct sql.NullBool
	)
	err := f.db.QueryRow(
		`SELECT future_return, max_adverse_excursion, max_favorable_excursion, was_direction_correct
		FROM calibration_outcomes WHERE jev_decision_id = ? AND horizon_minutes = ?`,
		decisionID, horizonMinutes,
	).Scan(&o.FutureReturn, &o.MaxAdverseExcursion, &o.MaxFavorableExcursion, &correct)
	if err != nil {
		t.Fatalf("query calibration outcome for decision %d (horizon %dm): %v", decisionID, horizonMinutes, err)
	}
	if correct.Valid {
		v := correct.Bool
		o.WasDirectionCorrect = &v
	}
	return o
}

func TestLabeler_HandleJob_LongDirectionCorrect(t *testing.T) {
	f := newLabelerFixtures(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	decision := f.insertDecision(t, base, domain.JevDirectionLong, 0.82)
	f.insertSnapshots(t, map[time.Duration]float64{
		0:               1000,   // entry
		1 * time.Minute: 1010,   // +1.0%
		2 * time.Minute: 990,    // -1.0%
		3 * time.Minute: 1005,   // +0.5%
		4 * time.Minute: 1020,   // +2.0%
		5 * time.Minute: 1004.1, // +0.41% (horizon boundary)
	}, base)

	if err := f.handleJob(t, decision.ID, 5); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	o := f.getOutcome(t, decision.ID, 5)
	if !almostEqual(o.FutureReturn, 0.41) {
		t.Fatalf("FutureReturn = %v, want ~0.41", o.FutureReturn)
	}
	if !almostEqual(o.MaxFavorableExcursion, 2.0) {
		t.Fatalf("MaxFavorableExcursion = %v, want ~2.0 (the +4min bar)", o.MaxFavorableExcursion)
	}
	if !almostEqual(o.MaxAdverseExcursion, -1.0) {
		t.Fatalf("MaxAdverseExcursion = %v, want ~-1.0 (the +2min bar)", o.MaxAdverseExcursion)
	}
	if o.WasDirectionCorrect == nil || !*o.WasDirectionCorrect {
		t.Fatalf("WasDirectionCorrect = %v, want true (positive future_return for LONG)", o.WasDirectionCorrect)
	}
}

func TestLabeler_HandleJob_ShortDirectionIncorrect(t *testing.T) {
	f := newLabelerFixtures(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	decision := f.insertDecision(t, base, domain.JevDirectionShort, 0.7)
	f.insertSnapshots(t, map[time.Duration]float64{
		0:               1000, // entry
		1 * time.Minute: 1005, // price rose: adverse to a SHORT
		5 * time.Minute: 1020, // +2.0% raw: price rose, bad for a SHORT
	}, base)

	if err := f.handleJob(t, decision.ID, 5); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	o := f.getOutcome(t, decision.ID, 5)
	if !almostEqual(o.FutureReturn, 2.0) {
		t.Fatalf("FutureReturn = %v, want ~2.0 (raw, direction-neutral)", o.FutureReturn)
	}
	// Direction-adjusted for SHORT: -2.0% is the worst point (MAE), 0% (entry, never favorable) is the best (MFE).
	if !almostEqual(o.MaxAdverseExcursion, -2.0) {
		t.Fatalf("MaxAdverseExcursion = %v, want ~-2.0", o.MaxAdverseExcursion)
	}
	if !almostEqual(o.MaxFavorableExcursion, 0) {
		t.Fatalf("MaxFavorableExcursion = %v, want 0 (price only ever rose)", o.MaxFavorableExcursion)
	}
	if o.WasDirectionCorrect == nil || *o.WasDirectionCorrect {
		t.Fatalf("WasDirectionCorrect = %v, want false (price rose against a SHORT)", o.WasDirectionCorrect)
	}
}

func TestLabeler_HandleJob_NoneDirectionLeavesWasDirectionCorrectNil(t *testing.T) {
	f := newLabelerFixtures(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	decision, err := f.decisions.Insert(context.Background(), domain.JevDecision{
		InstrumentID: f.instrument.ID, Symbol: f.instrument.Symbol, Timestamp: base,
		DecisionType: domain.JevDecisionTypeTrader, StateHash: "hash", StateJSON: "{}",
		QuestionVersion: "v1", ResponseJSON: "{}", Direction: ptr(domain.JevDirectionNone),
		ModelID: "test-model",
	})
	if err != nil {
		t.Fatalf("insert NONE decision: %v", err)
	}
	f.insertSnapshots(t, map[time.Duration]float64{
		0:               1000,
		5 * time.Minute: 1010,
	}, base)

	if err := f.handleJob(t, decision.ID, 5); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}

	o := f.getOutcome(t, decision.ID, 5)
	if o.WasDirectionCorrect != nil {
		t.Fatalf("WasDirectionCorrect = %v, want nil for direction=NONE", *o.WasDirectionCorrect)
	}
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
