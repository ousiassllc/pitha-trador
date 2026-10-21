package shadow

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
