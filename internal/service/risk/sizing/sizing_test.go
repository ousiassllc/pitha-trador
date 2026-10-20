package sizing_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/sizing"
)

func TestQuantity(t *testing.T) {
	limits := config.RiskLimits{
		InitialCapital:          10_000_000,
		MaxPositionPerSymbolPct: 2.0,  // 200,000 yen per symbol
		MaxTotalExposurePct:     20.0, // 2,000,000 yen in total
		MaxTradeLossPct:         0.25, // 25,000 yen per stop-out
	}
	cases := []struct {
		name         string
		price        float64
		stopLossPct  float64
		totalExposed float64
		wantQty      int64
		wantReason   string
	}{
		// 25,000 / (1000*0.6%) = 4166 shares by trade loss; 200 by symbol cap -> 200.
		{"symbol cap binds", 1000, 0.6, 0, 200, ""},
		// 25,000 / (5000*0.6%) = 833 by trade loss; 200,000/5000 = 40 by symbol -> not a full lot.
		{"symbol cap leaves no lot", 5000, 0.6, 0, 0, sizing.ReasonMaxPositionPerSymbolPct},
		// 25,000 / (1000*5%) = 500 shares by trade loss; symbol cap 200 -> 200.
		{"wide stop still capped by symbol", 1000, 5, 0, 200, ""},
		// 25,000 / (1000*20%) = 125 shares by trade loss -> one lot (100), not rounded up.
		{"trade loss binds and rounds down", 1000, 20, 0, 100, ""},
		// 25,000 / (1000*30%) = 83 shares -> below a lot.
		{"trade loss leaves no lot", 1000, 30, 0, 0, sizing.ReasonMaxTradeLossPct},
		// Only 0.5% of exposure headroom left = 50,000 yen = 50 shares at 1000.
		{"total exposure headroom binds", 1000, 0.6, 19.5, 0, sizing.ReasonMaxTotalExposurePct},
		{"exposure already at cap", 1000, 0.6, 20, 0, sizing.ReasonMaxTotalExposurePct},
		{"no stop means unbounded loss", 1000, 0, 0, 0, sizing.ReasonMaxTradeLossPct},
		{"non-positive price", 0, 0.6, 0, 0, sizing.ReasonInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			qty, reason := sizing.Quantity(limits, tc.stopLossPct, tc.price, tc.totalExposed)
			if qty != tc.wantQty || reason != tc.wantReason {
				t.Fatalf("Quantity = (%d, %q), want (%d, %q)", qty, reason, tc.wantQty, tc.wantReason)
			}
		})
	}
}

func TestQuantity_NoInitialCapitalRejects(t *testing.T) {
	limits := config.RiskLimits{MaxPositionPerSymbolPct: 2, MaxTotalExposurePct: 20, MaxTradeLossPct: 0.25}
	if qty, reason := sizing.Quantity(limits, 0.6, 1000, 0); qty != 0 || reason != sizing.ReasonInvalidInput {
		t.Fatalf("Quantity = (%d, %q), want (0, %q)", qty, reason, sizing.ReasonInvalidInput)
	}
}

// issue #804: with the Paper initial values (capital 500,000 yen, 100% per
// symbol, 100% in total) the two exposure limits alone leave room for one
// 100-share lot of a 5,000-yen stock (exactly 500,000 yen), so they never
// reject it. The unchanged max_trade_loss_pct does: 0.25% of 500,000 is
// 1,250 yen, but the 0.6% stop on 100 shares x 5,000 yen loses 3,000 yen.
func TestQuantity_Paper500kAt5000Yen(t *testing.T) {
	limits := config.RiskLimits{
		InitialCapital:          500_000,
		MaxPositionPerSymbolPct: 100,
		MaxTotalExposurePct:     100,
		MaxTradeLossPct:         0.25,
	}

	// The exposure limits alone: a trade-loss limit wide enough not to bind.
	exposureOnly := limits
	exposureOnly.MaxTradeLossPct = 100
	if qty, reason := sizing.Quantity(exposureOnly, sizing.DefaultStopLossPct, 5000, 0); qty != 100 || reason != "" {
		t.Errorf("exposure limits only: Quantity = (%d, %q), want (100, \"\")", qty, reason)
	}

	// Current Paper behaviour (max_trade_loss_pct 0.25 stays in risk.yaml): the
	// trade-loss limit is what stops the lot, not the capital or the exposure.
	if qty, reason := sizing.Quantity(limits, sizing.DefaultStopLossPct, 5000, 0); qty != 0 || reason != sizing.ReasonMaxTradeLossPct {
		t.Errorf("with max_trade_loss_pct 0.25: Quantity = (%d, %q), want (0, %q)", qty, reason, sizing.ReasonMaxTradeLossPct)
	}
	// The highest price that one lot passes max_trade_loss_pct at is 2,083 yen.
	if qty, _ := sizing.Quantity(limits, sizing.DefaultStopLossPct, 2083, 0); qty != 100 {
		t.Errorf("Quantity at 2,083 yen = %d, want 100", qty)
	}
	if qty, _ := sizing.Quantity(limits, sizing.DefaultStopLossPct, 2084, 0); qty != 0 {
		t.Errorf("Quantity at 2,084 yen = %d, want 0", qty)
	}
}
