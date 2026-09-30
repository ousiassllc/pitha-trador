package trading_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
)

// signalRepoFixture bundles a migrated test DB, a persisted instrument,
// and the repositories under test - Insert requires a real instruments
// (and, for the jev_decision_id FK, jev_decisions) row to satisfy the
// schema's foreign keys.
type signalRepoFixture struct {
	db           *sql.DB
	signals      *trading.SignalRepository
	instrumentID int64
}

func newSignalRepoFixture(t *testing.T) signalRepoFixture {
	t.Helper()
	db := newTestDB(t)

	return signalRepoFixture{
		db:           db,
		signals:      trading.NewSignalRepository(db),
		instrumentID: insertInstrument(t, db, "7203", "トヨタ自動車"),
	}
}

// insertDecision persists a minimal jev_decisions (decision_type=trader)
// row and returns its ID, for tests that need a real FK target for
// trade_signals.jev_decision_id.
func (f signalRepoFixture) insertDecision(t *testing.T, ts time.Time) int64 {
	t.Helper()
	return insertDecision(t, f.db, f.instrumentID, "7203", ts)
}

func TestSignalRepository_InsertAndGet_LongSignalWithJevDecision(t *testing.T) {
	f := newSignalRepoFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	decisionID := f.insertDecision(t, now)
	score := 0.82
	entryPrice := 2110.5

	created, err := f.signals.Insert(ctx, domain.TradeSignal{
		InstrumentID:        f.instrumentID,
		JevDecisionID:       &decisionID,
		Symbol:              "7203",
		Timestamp:           now,
		Direction:           domain.JevDirectionLong,
		Score:               &score,
		EntryPriceReference: &entryPrice,
		PolicyVersion:       "policy-v1",
		RiskPassed:          true,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("expected assigned ID, got 0")
	}
	if created.CreatedAt.IsZero() {
		t.Fatalf("expected created_at to be populated, got %+v", created)
	}

	got, err := f.signals.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", created.ID, err)
	}
	if got.Direction != domain.JevDirectionLong || got.PolicyVersion != "policy-v1" || !got.RiskPassed {
		t.Fatalf("Get(%d) = %+v, want Direction/PolicyVersion/RiskPassed to match Insert input", created.ID, got)
	}
	if got.JevDecisionID == nil || *got.JevDecisionID != decisionID {
		t.Fatalf("Get(%d).JevDecisionID = %v, want %d", created.ID, got.JevDecisionID, decisionID)
	}
	if got.Score == nil || *got.Score != score {
		t.Fatalf("Get(%d).Score = %v, want %v", created.ID, got.Score, score)
	}
	if got.EntryPriceReference == nil || *got.EntryPriceReference != entryPrice {
		t.Fatalf("Get(%d).EntryPriceReference = %v, want %v", created.ID, got.EntryPriceReference, entryPrice)
	}
	if got.RejectReason != nil {
		t.Fatalf("Get(%d).RejectReason = %v, want nil", created.ID, got.RejectReason)
	}
	if !got.Timestamp.Equal(now) {
		t.Fatalf("Get(%d).Timestamp = %v, want %v", created.ID, got.Timestamp, now)
	}
}

func TestSignalRepository_InsertAndGet_NoneSignalWithRejectReasonAndNoJevDecision(t *testing.T) {
	f := newSignalRepoFixture(t)
	ctx := context.Background()
	reason := "spread_too_wide: spread_bps=80.00 > max=50.00"

	created, err := f.signals.Insert(ctx, domain.TradeSignal{
		InstrumentID:  f.instrumentID,
		Symbol:        "7203",
		Timestamp:     time.Date(2026, 9, 27, 9, 32, 0, 0, time.UTC),
		Direction:     domain.JevDirectionNone,
		PolicyVersion: "policy-v1",
		RiskPassed:    false,
		RejectReason:  &reason,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := f.signals.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", created.ID, err)
	}
	if got.Direction != domain.JevDirectionNone || got.RiskPassed {
		t.Fatalf("Get(%d) = %+v, want Direction=NONE, RiskPassed=false", created.ID, got)
	}
	if got.JevDecisionID != nil {
		t.Fatalf("Get(%d).JevDecisionID = %v, want nil", created.ID, got.JevDecisionID)
	}
	if got.Score != nil {
		t.Fatalf("Get(%d).Score = %v, want nil", created.ID, got.Score)
	}
	if got.RejectReason == nil || *got.RejectReason != reason {
		t.Fatalf("Get(%d).RejectReason = %v, want %q", created.ID, got.RejectReason, reason)
	}
}

func TestSignalRepository_Get_NotFound(t *testing.T) {
	f := newSignalRepoFixture(t)

	_, err := f.signals.Get(context.Background(), 999999)
	if !errors.Is(err, trading.ErrSignalNotFound) {
		t.Fatalf("Get(unknown) error = %v, want ErrSignalNotFound", err)
	}
}

func TestSignalRepository_ListByInstrument_MostRecentFirstAndRespectsLimit(t *testing.T) {
	f := newSignalRepoFixture(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		_, err := f.signals.Insert(ctx, domain.TradeSignal{
			InstrumentID:  f.instrumentID,
			Symbol:        "7203",
			Timestamp:     base.Add(time.Duration(i) * time.Minute),
			Direction:     domain.JevDirectionNone,
			PolicyVersion: "policy-v1",
			RiskPassed:    false,
		})
		if err != nil {
			t.Fatalf("Insert(%d): %v", i, err)
		}
	}

	got, err := f.signals.ListByInstrument(ctx, f.instrumentID, 2)
	if err != nil {
		t.Fatalf("ListByInstrument: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(ListByInstrument()) = %d, want 2 (limit)", len(got))
	}
	if !got[0].Timestamp.Equal(base.Add(2 * time.Minute)) {
		t.Fatalf("ListByInstrument()[0].Timestamp = %v, want most recent", got[0].Timestamp)
	}
	if !got[1].Timestamp.Equal(base.Add(1 * time.Minute)) {
		t.Fatalf("ListByInstrument()[1].Timestamp = %v, want second most recent", got[1].Timestamp)
	}
}

func TestSignalRepository_ListRecent_SpansInstrumentsMostRecentFirstAndRespectsLimit(t *testing.T) {
	f := newSignalRepoFixture(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

	otherID := insertInstrument(t, f.db, "6758", "ソニーグループ")

	insert := func(instrumentID int64, symbol string, offset time.Duration) {
		t.Helper()
		_, err := f.signals.Insert(ctx, domain.TradeSignal{
			InstrumentID: instrumentID, Symbol: symbol, Timestamp: base.Add(offset),
			Direction: domain.JevDirectionNone, PolicyVersion: "policy-v1",
		})
		if err != nil {
			t.Fatalf("Insert(%s, %v): %v", symbol, offset, err)
		}
	}
	insert(f.instrumentID, "7203", 0)
	insert(otherID, "6758", time.Minute)
	insert(f.instrumentID, "7203", 2*time.Minute)

	got, err := f.signals.ListRecent(ctx, 2)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(ListRecent()) = %d, want 2 (limit)", len(got))
	}
	if got[0].Symbol != "7203" || !got[0].Timestamp.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("ListRecent()[0] = %s@%v, want 7203 at most recent", got[0].Symbol, got[0].Timestamp)
	}
	if got[1].Symbol != "6758" || !got[1].Timestamp.Equal(base.Add(time.Minute)) {
		t.Fatalf("ListRecent()[1] = %s@%v, want 6758 second most recent", got[1].Symbol, got[1].Timestamp)
	}
}

func TestSignalRepository_CountDirectional_ExcludesNoneSignals(t *testing.T) {
	f := newSignalRepoFixture(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

	for i, dir := range []string{domain.JevDirectionLong, domain.JevDirectionShort, domain.JevDirectionNone, domain.JevDirectionNone} {
		_, err := f.signals.Insert(ctx, domain.TradeSignal{
			InstrumentID: f.instrumentID, Symbol: "7203", Timestamp: base.Add(time.Duration(i) * time.Minute),
			Direction: dir, PolicyVersion: "policy-v1",
		})
		if err != nil {
			t.Fatalf("Insert(%s): %v", dir, err)
		}
	}

	got, err := f.signals.CountDirectional(ctx)
	if err != nil {
		t.Fatalf("CountDirectional: %v", err)
	}
	if got != 2 {
		t.Fatalf("CountDirectional() = %d, want 2 (LONG+SHORT only)", got)
	}
}
