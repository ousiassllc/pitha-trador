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
