package monitorflow_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

type fakeCounter struct{ n int }

func (f *fakeCounter) ConsecutiveFailures() int { return f.n }

type monitorFixture struct {
	engine    *risk.Engine
	notifier  *fakeNotifier
	closer    *fakeCloser
	instrID   int64
	orders    *trading.OrderRepository
	positions *trading.PositionRepository
}

func newMonitorFixture(t *testing.T, cfg func(*risk.Config)) *monitorFixture {
	t.Helper()
	db := newTestDB(t)
	f := &monitorFixture{
		notifier:  &fakeNotifier{},
		closer:    &fakeCloser{},
		orders:    trading.NewOrderRepository(db),
		positions: trading.NewPositionRepository(db),
	}
	inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	f.instrID = inst.ID
	c := risk.Config{
		Limits:     testLimits(),
		KillSwitch: system.NewKillSwitchRepository(db),
		Settings:   system.NewRuntimeSettingsRepository(db),
		Portfolio:  risk.ZeroPortfolioProvider{},
		Positions:  f.positions,
		Orders:     f.orders,
		Closer:     f.closer,
		Notifier:   f.notifier,
	}
	if cfg != nil {
		cfg(&c)
	}
	f.engine = risk.NewEngine(c)
	return f
}

// openPosition inserts an entry order and the position it opened.
func (f *monitorFixture) openPosition(t *testing.T, order domain.PaperOrder, position domain.Position) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	order.InstrumentID, order.Symbol, order.SubmittedAt = f.instrID, "7203", now
	if order.OrderType == "" {
		order.OrderType = domain.OrderTypeMarket
	}
	inserted, err := f.orders.Insert(ctx, order)
	if err != nil {
		t.Fatalf("insert order: %v", err)
	}
	position.InstrumentID, position.EntryOrderID, position.Symbol, position.OpenedAt = f.instrID, inserted.ID, "7203", now
	position.CurrentPrice = position.EntryPrice
	if _, err := f.positions.Open(ctx, position); err != nil {
		t.Fatalf("open position: %v", err)
	}
}

func filledOrder(side string, qty int64, price float64) domain.PaperOrder {
	now := time.Now().UTC()
	return domain.PaperOrder{Side: side, Quantity: qty, Status: domain.OrderStatusFilled, FilledAt: &now, FilledPrice: &price}
}

func longPosition(qty int64, price float64) domain.Position {
	return domain.Position{Side: domain.PositionSideLong, Quantity: qty, EntryPrice: price}
}

func (f *monitorFixture) unresolvedReasons(t *testing.T) []string {
	t.Helper()
	_, events, err := f.engine.State(context.Background())
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	reasons := make([]string, len(events))
	for i, ev := range events {
		reasons[i] = ev.Reason
	}
	return reasons
}

func assertReasons(t *testing.T, got []string, want ...string) {
	t.Helper()
	got = slices.Sorted(slices.Values(got))
	want = slices.Sorted(slices.Values(want))
	if !slices.Equal(got, want) {
		t.Fatalf("unresolved reasons = %v, want %v", got, want)
	}
}

func TestEngine_CheckBrokerAPIHealth_TriggersAtThresholdOnly(t *testing.T) {
	counter := &fakeCounter{}
	f := newMonitorFixture(t, func(c *risk.Config) { c.BrokerAPIFailures = counter; c.FailureThreshold = 3 })
	ctx := context.Background()

	counter.n = 2
	if err := f.engine.CheckBrokerAPIHealth(ctx); err != nil {
		t.Fatalf("CheckBrokerAPIHealth: %v", err)
	}
	assertReasons(t, f.unresolvedReasons(t))

	counter.n = 3
	for range 2 { // idempotent while the streak persists
		if err := f.engine.CheckBrokerAPIHealth(ctx); err != nil {
			t.Fatalf("CheckBrokerAPIHealth: %v", err)
		}
	}
	assertReasons(t, f.unresolvedReasons(t), domain.KillReasonBrokerAPIError)
	if len(f.notifier.triggered) != 1 || f.notifier.triggered[0].autoResumable {
		t.Fatalf("notifications = %+v, want one manual-resume-only broker_api_error alert", f.notifier.triggered)
	}
}

func TestEngine_CheckDBWriteHealth_TriggersAtDefaultThreshold(t *testing.T) {
	counter := &fakeCounter{n: risk.DefaultFailureThreshold - 1}
	f := newMonitorFixture(t, func(c *risk.Config) { c.DBWriteFailures = counter })
	ctx := context.Background()

	if err := f.engine.CheckDBWriteHealth(ctx); err != nil {
		t.Fatalf("CheckDBWriteHealth: %v", err)
	}
	assertReasons(t, f.unresolvedReasons(t))

	counter.n = risk.DefaultFailureThreshold
	if err := f.engine.CheckDBWriteHealth(ctx); err != nil {
		t.Fatalf("CheckDBWriteHealth: %v", err)
	}
	assertReasons(t, f.unresolvedReasons(t), domain.KillReasonDBWriteFailure)
}

func TestEngine_FailureStreakChecksAreNoOpWithoutCounters(t *testing.T) {
	f := newMonitorFixture(t, nil)
	if err := f.engine.RunPeriodicChecks(context.Background()); err != nil {
		t.Fatalf("RunPeriodicChecks: %v", err)
	}
	assertReasons(t, f.unresolvedReasons(t))
}

func TestEngine_CheckPositionReconciliation(t *testing.T) {
	limitPrice := 100.0
	tests := []struct {
		name     string
		order    domain.PaperOrder
		position domain.Position
		want     []string
	}{
		{
			name:     "consistent long fill raises nothing",
			order:    filledOrder(domain.OrderSideBuy, 100, 1000),
			position: longPosition(100, 1000),
		},
		{
			name:     "entry order never filled is an unexpected position",
			order:    domain.PaperOrder{Side: domain.OrderSideBuy, Quantity: 100, Status: domain.OrderStatusPending},
			position: longPosition(100, 1000),
			want:     []string{domain.KillReasonUnexpectedPosition},
		},
		{
			name:     "buy order behind a short position is an unexpected position",
			order:    filledOrder(domain.OrderSideBuy, 100, 1000),
			position: domain.Position{Side: domain.PositionSideShort, Quantity: 100, EntryPrice: 1000},
			want:     []string{domain.KillReasonUnexpectedPosition},
		},
		{
			name:     "quantity differing from the fill is a fill discrepancy",
			order:    filledOrder(domain.OrderSideBuy, 100, 1000),
			position: longPosition(200, 1000),
			want:     []string{domain.KillReasonFillDiscrepancy},
		},
		{
			name:     "entry price differing from the fill is a fill discrepancy",
			order:    filledOrder(domain.OrderSideBuy, 100, 1000),
			position: longPosition(100, 1010),
			want:     []string{domain.KillReasonFillDiscrepancy},
		},
		{
			name: "buy limit filled above its limit is a fill discrepancy",
			order: func() domain.PaperOrder {
				o := filledOrder(domain.OrderSideBuy, 100, 105)
				o.OrderType, o.LimitPrice = domain.OrderTypeLimit, &limitPrice
				return o
			}(),
			position: longPosition(100, 105),
			want:     []string{domain.KillReasonFillDiscrepancy},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMonitorFixture(t, nil)
			f.openPosition(t, tt.order, tt.position)
			ctx := context.Background()

			for range 2 { // idempotent across periodic runs
				if err := f.engine.CheckPositionReconciliation(ctx); err != nil {
					t.Fatalf("CheckPositionReconciliation: %v", err)
				}
			}
			assertReasons(t, f.unresolvedReasons(t), tt.want...)
			if len(tt.want) > 0 && len(f.closer.closed) != 1 {
				t.Fatalf("closer.closed = %v, want open positions force-closed exactly once", f.closer.closed)
			}
		})
	}
}

// mutableHealth is a HealthChecker whose answer can flip after construction.
type mutableHealth struct{ healthy bool }

func (h *mutableHealth) Healthy(context.Context) (bool, error) { return h.healthy, nil }

func TestEngine_RunPeriodicChecks_RunsEveryDetectorAndJoinsNoErrors(t *testing.T) {
	market := &mutableHealth{healthy: false}
	jev := &mutableHealth{healthy: false}
	f := newMonitorFixture(t, func(c *risk.Config) {
		c.MarketDataHealth, c.JevAPIHealth = market, jev
		c.BrokerAPIFailures, c.DBWriteFailures = &fakeCounter{n: 99}, &fakeCounter{n: 99}
		c.Portfolio = fakePortfolio{dailyLossPct: 0.85} // 85% of testLimits' 1.0 max
	})
	f.openPosition(t, filledOrder(domain.OrderSideBuy, 100, 1000), longPosition(200, 1000))

	if err := f.engine.RunPeriodicChecks(context.Background()); err != nil {
		t.Fatalf("RunPeriodicChecks: %v", err)
	}

	assertReasons(t, f.unresolvedReasons(t),
		domain.KillReasonMarketDataDown, domain.KillReasonJevAPIDown,
		domain.KillReasonBrokerAPIError, domain.KillReasonDBWriteFailure,
		domain.KillReasonFillDiscrepancy)
	if len(f.notifier.dailyLoss) != 1 {
		t.Fatalf("daily-loss warnings = %d, want 1 (85%% of the limit is past the 80%% threshold)", len(f.notifier.dailyLoss))
	}
}

// insertOrphanFill records a FILLED order at filledAt that no position
// references: the partial-failure state issue #158 describes.
func (f *monitorFixture) insertOrphanFill(t *testing.T, filledAt time.Time) {
	t.Helper()
	ctx := context.Background()
	order, err := f.orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: f.instrID, Symbol: "7203", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket,
		Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: filledAt,
	})
	if err != nil {
		t.Fatalf("insert order: %v", err)
	}
	if _, err := f.orders.Fill(ctx, order.ID, 2100, nil, filledAt); err != nil {
		t.Fatalf("fill order: %v", err)
	}
}

func TestEngine_CheckPositionReconciliation_TriggersOnFilledOrderWithoutPosition(t *testing.T) {
	f := newMonitorFixture(t, nil)
	f.insertOrphanFill(t, time.Now().UTC().Add(-5*time.Minute))

	if err := f.engine.CheckPositionReconciliation(context.Background()); err != nil {
		t.Fatalf("CheckPositionReconciliation: %v", err)
	}

	assertReasons(t, f.unresolvedReasons(t), domain.KillReasonFillDiscrepancy)
}

func TestEngine_CheckPositionReconciliation_IgnoresJustFilledOrderAndLinkedOrders(t *testing.T) {
	f := newMonitorFixture(t, nil)
	// Filled a few seconds ago: an exit order is filled just before its
	// position row is closed, so it gets a grace period.
	f.insertOrphanFill(t, time.Now().UTC().Add(-5*time.Second))
	// A properly linked entry fill is never an orphan.
	f.openPosition(t, filledOrder(domain.OrderSideBuy, 100, 2100), longPosition(100, 2100))

	if err := f.engine.CheckPositionReconciliation(context.Background()); err != nil {
		t.Fatalf("CheckPositionReconciliation: %v", err)
	}

	assertReasons(t, f.unresolvedReasons(t))
}
