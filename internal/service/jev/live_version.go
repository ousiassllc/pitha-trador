package jev

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
)

// InputPriceUSDPerMtok is TypeSafe's published price of Jev 1.13 / 1.14 per
// million INPUT tokens (USD 0.042; output tokens are free), from
// https://docs.typesafe.ai/models as read on 2026-10-22. The API returns no
// charge, only usage (docs/architecture/overview/integrations.md §6), so the
// shadow's request_cost is this price times usage.input_tokens: an estimate
// of the vendor's price list, not a figure checked against an invoice. If the
// vendor changes the price, this is the one constant to update.
const InputPriceUSDPerMtok = 0.042

// InputCostUSD is the estimated cost in USD of a call that used tokens input
// tokens (InputPriceUSDPerMtok).
func InputCostUSD(tokens int) float64 {
	return float64(tokens) * InputPriceUSDPerMtok / 1e6
}

// LiveVersion returns the live Trader prompt version: the one a promotion
// stored in runtime_settings, else the build default TraderQuestionVersion. A
// stored version this build does not know (for example after a downgrade)
// falls back to the build default and is logged: an unknown name must never be
// sent. A nil store is the build default.
func LiveVersion(ctx context.Context, store jevshadowsettings.Store) string {
	if store == nil {
		return TraderQuestionVersion
	}
	stored := jevshadowsettings.LoadLiveVersion(ctx, store)
	switch {
	case stored == "":
		return TraderQuestionVersion
	case !IsTraderVersion(stored):
		slog.Error("jev: the stored live trader prompt version is not registered in this build; using the build default",
			"stored", stored, "default", TraderQuestionVersion)
		return TraderQuestionVersion
	}
	return stored
}
