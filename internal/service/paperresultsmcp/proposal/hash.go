package proposal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// canonical is what the request hash is taken over: every content member in a
// fixed order, normalized. idempotency_key is the lookup key, not content.
type canonical struct {
	Symbol          string   `json:"symbol"`
	Bucket          string   `json:"bucket"`
	Action          string   `json:"action"`
	Quantity        *int64   `json:"quantity"`
	BudgetJPY       *float64 `json:"budget_jpy"`
	OrderType       string   `json:"order_type"`
	LimitPrice      *float64 `json:"limit_price"`
	StopLossPrice   *float64 `json:"stop_loss_price"`
	TakeProfitPrice *float64 `json:"take_profit_price"`
	ExitPlan        *string  `json:"exit_plan"`
	MaxLossJPY      *float64 `json:"max_loss_jpy"`
	Rationale       string   `json:"rationale"`
	Sources         []Source `json:"sources"`
	ExpiresAt       string   `json:"expires_at"`
}

// Hash is the hex SHA-256 of the normalized content. Two requests with the
// same idempotency_key are the same proposal exactly when their hashes match.
func (r Request) Hash() string {
	b, err := json.Marshal(canonical{
		Symbol: r.Symbol, Bucket: r.Bucket, Action: r.Action, Quantity: r.Quantity, BudgetJPY: r.BudgetJPY,
		OrderType: r.OrderType, LimitPrice: r.LimitPrice, StopLossPrice: r.StopLossPrice,
		TakeProfitPrice: r.TakeProfitPrice, ExitPlan: r.ExitPlan, MaxLossJPY: r.MaxLossJPY,
		Rationale: r.Rationale, Sources: r.Sources, ExpiresAt: r.ExpiresAt.Format(time.RFC3339),
	})
	if err != nil { // plain strings and finite numbers: cannot fail
		panic("proposal: marshal canonical request: " + err.Error())
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// SourcesJSON is the sources as stored in order_proposals.sources_json.
func (r Request) SourcesJSON() string {
	b, err := json.Marshal(r.Sources)
	if err != nil {
		panic("proposal: marshal sources: " + err.Error())
	}
	return string(b)
}
