package shadow

// InputPriceUSDPerMtok is TypeSafe's published price of Jev 1.13 / 1.14 per
// million INPUT tokens (USD 0.042; output tokens are free), from
// https://docs.typesafe.ai/models as read on 2026-10-22. The number is a user
// decision (確定). The API returns no charge, only usage
// (docs/architecture/overview/integrations.md §6), so the shadow's request_cost
// is this price times the input tokens: an estimate from the vendor's
// published price list, NOT a figure checked against an invoice. If the vendor
// changes the price, this is the one constant to update.
const InputPriceUSDPerMtok = 0.042

// InputCostUSD is the estimated cost in USD of a call that used tokens input
// tokens (InputPriceUSDPerMtok).
func InputCostUSD(tokens int) float64 {
	return float64(tokens) * InputPriceUSDPerMtok / 1e6
}

// BytesPerTokenFloor is the conservative characters-per-token ratio used when
// a response carries no usage: the request's size in bytes divided by this
// gives the estimated input tokens. It is deliberately low (more tokens, so a
// higher cost than reality is the likelier error): the request is JSON of
// numbers and short keys plus Japanese text (3 bytes a character in UTF-8,
// about 1 token a character), which tokenizes at roughly 2.5-3 bytes a
// token; 2 stays below that. It is an assumption, not a measurement against
// the vendor's tokenizer or an invoice.
const BytesPerTokenFloor = 2

// EstimateInputTokens estimates the input tokens of a request of requestBytes
// bytes (ceil(requestBytes / BytesPerTokenFloor), at least 1). The shadow
// uses it for a response without usage so that the call is counted against
// the daily cost cap instead of as 0 USD.
func EstimateInputTokens(requestBytes int) int {
	return max(1, (requestBytes+BytesPerTokenFloor-1)/BytesPerTokenFloor)
}
