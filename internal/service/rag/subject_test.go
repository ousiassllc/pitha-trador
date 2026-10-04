package rag_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

type subjectFixture struct {
	svc       *rag.Service
	snapshots *market.SnapshotRepository
	decisions *judgement.DecisionRepository
	outcomes  *judgement.CalibrationRepository
	insts     map[string]domain.Instrument
}

func newSubjectFixture(t *testing.T, symbols ...string) subjectFixture {
	t.Helper()
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	snapshots := market.NewSnapshotRepository(db)
	decisions := judgement.NewDecisionRepository(db)
	f := subjectFixture{
		svc:       rag.NewService(db, decisions, snapshots),
		snapshots: snapshots,
		decisions: decisions,
		outcomes:  judgement.NewCalibrationRepository(db),
		insts:     map[string]domain.Instrument{},
	}
	for _, s := range symbols {
		f.insts[s] = mustCreateInstrument(t, instruments, s)
	}
	return f
}

func (f subjectFixture) indexSnapshot(t *testing.T, symbol string, ts time.Time, in rag.FeatureInput) {
	t.Helper()
	inst := f.insts[symbol]
	snap, err := f.snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Symbol: symbol, Timestamp: ts,
		Price: 1000, Volume: 100, Turnover: 100_000,
		Feature: domain.Feature{VWAP: 995}, RawDataJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	if err := f.svc.IndexSnapshot(context.Background(), snap.ID, in); err != nil {
		t.Fatalf("IndexSnapshot: %v", err)
	}
}

func (f subjectFixture) indexDecision(t *testing.T, symbol, decisionType string, ts time.Time, in rag.FeatureInput) domain.JevDecision {
	t.Helper()
	d := domain.JevDecision{
		InstrumentID: f.insts[symbol].ID, Symbol: symbol, Timestamp: ts,
		DecisionType: decisionType, StateHash: "h", StateJSON: `{}`, QuestionVersion: "test",
		ResponseJSON: `{}`, ModelID: "jev-test",
	}
	if decisionType == domain.JevDecisionTypeTrader {
		direction, confidence := domain.JevDirectionLong, 0.8
		d.Direction, d.Confidence = &direction, &confidence
	}
	saved, err := f.decisions.Insert(context.Background(), d)
	if err != nil {
		t.Fatalf("insert decision: %v", err)
	}
	if err := f.svc.IndexDecision(context.Background(), saved.ID, in); err != nil {
		t.Fatalf("IndexDecision: %v", err)
	}
	return saved
}

// FR-RAG-4: at cold start the only indexed snapshot is the current one;
// it must not come back as a "similar past case".
func TestService_Context_ExcludesCurrentSnapshotSoColdStartIsEmpty(t *testing.T) {
	f := newSubjectFixture(t, "7203")
	now := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)
	in := rag.FeatureInput{Return1m: ptr(0.01)}
	f.indexSnapshot(t, "7203", now, in)

	got, err := f.svc.Context(context.Background(), in, rag.Subject{Symbol: "7203", Timestamp: now}, rag.DefaultK)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(got.Cases) != 0 {
		t.Errorf("Context().Cases = %+v, want empty: only the current state is indexed (FR-RAG-4)", got.Cases)
	}
}

func TestService_Context_SnapshotRecencyGuardIsPerSymbol(t *testing.T) {
	f := newSubjectFixture(t, "7203", "9433")
	now := time.Date(2026, 9, 28, 1, 30, 0, 0, time.UTC)
	in := rag.FeatureInput{Return1m: ptr(0.01)}

	f.indexSnapshot(t, "7203", now, in)                                  // current
	f.indexSnapshot(t, "7203", now.Add(-time.Minute), in)                // inside guard
	f.indexSnapshot(t, "7203", now.Add(-rag.SnapshotRecencyGuard+1), in) // just inside guard
	f.indexSnapshot(t, "7203", now.Add(-rag.SnapshotRecencyGuard), in)   // exactly at the guard edge: usable
	f.indexSnapshot(t, "7203", now.Add(-rag.SnapshotRecencyGuard-time.Minute), in)
	f.indexSnapshot(t, "9433", now, in) // other symbol, same time: a genuine peer case

	got, err := f.svc.Context(context.Background(), in, rag.Subject{Symbol: "7203", Timestamp: now}, 10)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	type key struct {
		symbol string
		ts     time.Time
	}
	want := map[key]bool{
		{"7203", now.Add(-rag.SnapshotRecencyGuard)}:               true,
		{"7203", now.Add(-rag.SnapshotRecencyGuard - time.Minute)}: true,
		{"9433", now}: true,
	}
	if len(got.Cases) != len(want) {
		t.Fatalf("Context().Cases = %+v, want exactly %d cases (same-symbol bars inside the guard excluded)", got.Cases, len(want))
	}
	for _, c := range got.Cases {
		if !want[key{c.Symbol, c.Timestamp}] {
			t.Errorf("unexpected case %+v: same-symbol snapshots within the recency guard must be excluded", c)
		}
	}
}

// A Trader call follows its own Scout decision (same symbol/timestamp,
// distance ~0); that decision is not a past case, while an earlier one is.
func TestService_Context_ExcludesOwnScoutDecisionKeepsEarlierOnes(t *testing.T) {
	f := newSubjectFixture(t, "7203")
	now := time.Date(2026, 9, 28, 1, 30, 0, 0, time.UTC)
	in := rag.FeatureInput{Return1m: ptr(0.01)}

	own := f.indexDecision(t, "7203", domain.JevDecisionTypeScout, now, in)
	earlier := f.indexDecision(t, "7203", domain.JevDecisionTypeScout, now.Add(-time.Hour), in)
	ownLabeled := f.indexDecision(t, "7203", domain.JevDecisionTypeTrader, now, in)
	if _, err := f.outcomes.Insert(context.Background(), domain.CalibrationOutcome{
		JevDecisionID: ownLabeled.ID, HorizonMinutes: 5, FutureReturn: 0.1,
	}); err != nil {
		t.Fatalf("insert outcome: %v", err)
	}

	got, err := f.svc.Context(context.Background(), in, rag.Subject{Symbol: "7203", Timestamp: now}, rag.DefaultK)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(got.Cases) != 1 || !got.Cases[0].Timestamp.Equal(earlier.Timestamp) {
		t.Fatalf("Context().Cases = %+v, want only the earlier decision (own decisions %d/%d excluded)", got.Cases, own.ID, ownLabeled.ID)
	}
}
