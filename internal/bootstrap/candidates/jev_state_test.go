package candidates

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
)

func mustInsertPassingSnapshot(t *testing.T, r *Refresher, inst domain.Instrument) {
	t.Helper()
	if _, err := r.Snapshots.InsertBatch(context.Background(), []domain.Snapshot{{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: time.Now().UTC(),
		Price: 2500, Volume: 1000, Turnover: 2_500_000, SpreadBps: ptrF(10),
		Feature: domain.Feature{
			VWAP: 2490, PriceVsVWAPBps: 40,
			VolumeRatio5m: ptrF(1.5), Return5m: ptrF(0.5), RealizedVol5m: ptrF(0.01),
		},
	}}); err != nil {
		t.Fatalf("InsertBatch %s: %v", inst.Symbol, err)
	}
}

func mustInsertDecision(t *testing.T, r *Refresher, inst domain.Instrument, decisionType string, at time.Time, direction string, confidence float64, entryQuality string) {
	t.Helper()
	d := domain.JevDecision{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: at, DecisionType: decisionType,
		StateHash: "h", StateJSON: "{}", QuestionVersion: "v1", ModelID: "m",
		ResponseJSON: `{"entry_quality":"` + entryQuality + `"}`,
	}
	if decisionType == domain.JevDecisionTypeTrader {
		d.Direction = &direction
		d.Confidence = &confidence
	}
	if _, err := r.Decisions.Insert(context.Background(), d); err != nil {
		t.Fatalf("Insert decision %s/%s: %v", inst.Symbol, decisionType, err)
	}
}

func mustInsertFilledOrder(t *testing.T, orders *trading.OrderRepository, inst domain.Instrument, side string, quantity int64) domain.PaperOrder {
	t.Helper()
	now := time.Now().UTC()
	created, err := orders.Insert(context.Background(), domain.PaperOrder{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Side: side, OrderType: domain.OrderTypeMarket,
		Quantity: quantity, Status: domain.OrderStatusPending, SubmittedAt: now,
	})
	if err != nil {
		t.Fatalf("Insert order %s: %v", inst.Symbol, err)
	}
	filled, err := orders.Fill(context.Background(), created.ID, 2500, nil, now)
	if err != nil {
		t.Fatalf("Fill order %s: %v", inst.Symbol, err)
	}
	return filled
}

func mustOpenPosition(t *testing.T, r *Refresher, orders *trading.OrderRepository, inst domain.Instrument, side string, quantity int64) {
	t.Helper()
	entry := mustInsertFilledOrder(t, orders, inst, domain.OrderSideBuy, quantity)
	if _, err := r.Positions.Open(context.Background(), domain.Position{
		InstrumentID: inst.ID, EntryOrderID: entry.ID, Symbol: inst.Symbol, Side: side, Quantity: quantity,
		EntryPrice: 2500, CurrentPrice: 2500, OpenedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Open position %s: %v", inst.Symbol, err)
	}
}

func candidateBySymbol(t *testing.T, r *Refresher, symbol string) domain.Candidate {
	t.Helper()
	cs, _, err := r.Screener.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	for _, c := range cs {
		if c.Symbol == symbol {
			return c
		}
	}
	t.Fatalf("candidate %q not published; got %d candidates", symbol, len(cs))
	return domain.Candidate{}
}

// issue #492: the Scanner Dashboard's Jev Direction/Confidence/Entry
// Quality/Current Position columns must carry the latest Trader decision
// and the signed open position through Refresh.
func TestRefresh_AttachesLatestTraderDecisionAndSignedPositionToCandidates(t *testing.T) {
	r, orders := newTestRefresherWithOrders(t)
	r.Strategy.FastScreener = lenientFastScreener()
	base := time.Now().UTC().Add(-time.Hour)

	long := mustCreateInstrument(t, r, "1001")
	short := mustCreateInstrument(t, r, "1002")
	pending := mustCreateInstrument(t, r, "1003")
	scoutOnly := mustCreateInstrument(t, r, "1004")
	for _, inst := range []domain.Instrument{long, short, pending, scoutOnly} {
		mustInsertPassingSnapshot(t, r, inst)
	}

	// An older Trader decision must lose to the newer one; a Scout row
	// newer than both must not be mistaken for the Trader decision.
	mustInsertDecision(t, r, long, domain.JevDecisionTypeTrader, base, domain.JevDirectionShort, 0.30, "poor")
	mustInsertDecision(t, r, long, domain.JevDecisionTypeTrader, base.Add(time.Minute), domain.JevDirectionLong, 0.74, "strong")
	mustInsertDecision(t, r, long, domain.JevDecisionTypeScout, base.Add(2*time.Minute), "", 0, "exceptional")
	mustInsertDecision(t, r, short, domain.JevDecisionTypeTrader, base, domain.JevDirectionNone, 0.55, "fair")
	mustInsertDecision(t, r, scoutOnly, domain.JevDecisionTypeScout, base, "", 0, "strong")

	mustOpenPosition(t, r, orders, long, domain.PositionSideLong, 100)
	mustOpenPosition(t, r, orders, short, domain.PositionSideShort, 200)

	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	got := candidateBySymbol(t, r, "1001")
	if got.JevDirection == nil || *got.JevDirection != domain.JevDirectionLong {
		t.Errorf("1001 JevDirection = %v, want LONG (latest Trader decision)", got.JevDirection)
	}
	if got.JevConfidence == nil || *got.JevConfidence != 0.74 {
		t.Errorf("1001 JevConfidence = %v, want 0.74", got.JevConfidence)
	}
	if got.EntryQuality == nil || *got.EntryQuality != "strong" {
		t.Errorf("1001 EntryQuality = %v, want strong", got.EntryQuality)
	}
	if got.CurrentPosition == nil || *got.CurrentPosition != 100 {
		t.Errorf("1001 CurrentPosition = %v, want +100 for a LONG position", got.CurrentPosition)
	}

	got = candidateBySymbol(t, r, "1002")
	if got.JevDirection == nil || *got.JevDirection != domain.JevDirectionNone {
		t.Errorf("1002 JevDirection = %v, want NONE", got.JevDirection)
	}
	if got.CurrentPosition == nil || *got.CurrentPosition != -200 {
		t.Errorf("1002 CurrentPosition = %v, want -200 for a SHORT position", got.CurrentPosition)
	}

	for _, symbol := range []string{"1003", "1004"} {
		got = candidateBySymbol(t, r, symbol)
		if got.JevDirection != nil || got.JevConfidence != nil || got.EntryQuality != nil || got.CurrentPosition != nil {
			t.Errorf("%s = direction %v confidence %v quality %v position %v, want all nil (no Trader decision, flat)",
				symbol, got.JevDirection, got.JevConfidence, got.EntryQuality, got.CurrentPosition)
		}
	}
}

// A decision or position recorded after the first cycle shows up on the
// next one (the 15-30s cycle is what keeps the Dashboard live), and a
// closed position drops back to a nil CurrentPosition.
func TestRefresh_PicksUpDecisionAndPositionChangesOnNextCycle(t *testing.T) {
	r, orders := newTestRefresherWithOrders(t)
	r.Strategy.FastScreener = lenientFastScreener()
	inst := mustCreateInstrument(t, r, "1001")
	mustInsertPassingSnapshot(t, r, inst)

	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh #1: %v", err)
	}
	if got := candidateBySymbol(t, r, "1001"); got.JevDirection != nil || got.CurrentPosition != nil {
		t.Fatalf("before any decision: direction %v position %v, want nil", got.JevDirection, got.CurrentPosition)
	}

	mustInsertDecision(t, r, inst, domain.JevDecisionTypeTrader, time.Now().UTC(), domain.JevDirectionLong, 0.8, "good")
	mustOpenPosition(t, r, orders, inst, domain.PositionSideLong, 100)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh #2: %v", err)
	}
	got := candidateBySymbol(t, r, "1001")
	if got.JevDirection == nil || *got.JevDirection != domain.JevDirectionLong || got.CurrentPosition == nil || *got.CurrentPosition != 100 {
		t.Fatalf("after decision+position: direction %v position %v, want LONG/100", got.JevDirection, got.CurrentPosition)
	}

	open, err := r.Positions.GetOpenByInstrument(context.Background(), inst.ID)
	if err != nil {
		t.Fatalf("GetOpenByInstrument: %v", err)
	}
	exit := mustInsertFilledOrder(t, orders, inst, domain.OrderSideSell, 100)
	if _, err := r.Positions.Close(context.Background(), open.ID, exit.ID, 2600, 10000, domain.ExitReasonManual, time.Now().UTC()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh #3: %v", err)
	}
	if got := candidateBySymbol(t, r, "1001"); got.CurrentPosition != nil {
		t.Errorf("after close: CurrentPosition = %v, want nil", *got.CurrentPosition)
	}
}
