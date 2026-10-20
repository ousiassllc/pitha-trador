package risk

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/bucket"
)

// Budget-check rejection reasons of a new entry (issue #824,
// internal/service/risk/bucket).
const (
	ReasonBucketDisabled       = bucket.ReasonDisabled
	ReasonBucketBudgetExceeded = bucket.ReasonBudgetExceeded
)

// StockBucket supplies the Paper stock bucket's budget and holdings for
// Check's budget gates. bucket.Provider is the production implementation. An
// Engine built without one (every Live Engine) has no budget gate: Live keeps
// one whole-account bucket.
type StockBucket interface {
	// StockBudgetJPY is the stock bucket's budget in force now, for an
	// operating capital of capitalJPY.
	StockBudgetJPY(ctx context.Context, capitalJPY float64) (float64, error)
	// StockHeldJPY is what the stock bucket holds now: the market value of
	// its open positions (bucket.Notional, as the exposure limits measure).
	StockHeldJPY(ctx context.Context) (float64, error)
}

// gateStockBucket is Check's first budget gate: it reads the stock bucket's
// budget and rejects every entry while it is 0 (the bucket is not used). The
// budget is returned for checkStockBucketBudget. Without a StockBucket it
// passes.
func (e *Engine) gateStockBucket(ctx context.Context, capitalJPY float64) (budget float64, ok bool, reason string) {
	if e.stockBucket == nil {
		return 0, true, ""
	}
	budget, err := e.stockBucket.StockBudgetJPY(ctx, capitalJPY)
	if err != nil {
		ok, reason = e.failClosed("read stock bucket budget", err)
		return 0, ok, reason
	}
	if bucket.Disabled(budget) {
		return budget, false, bucket.DisabledMessage(domain.BucketStock, budget)
	}
	return budget, true, ""
}

// checkStockBucketBudget is Check's second budget gate, run once the entry's
// size is known: what the stock bucket holds plus this entry (qty at price)
// may not exceed budget. It passes without a StockBucket.
func (e *Engine) checkStockBucketBudget(ctx context.Context, budget float64, qty int64, price float64) (bool, string) {
	if e.stockBucket == nil {
		return true, ""
	}
	held, err := e.stockBucket.StockHeldJPY(ctx)
	if err != nil {
		return e.failClosed("read stock bucket holdings", err)
	}
	if amount := float64(qty) * price; bucket.Exceeded(held, amount, budget) {
		return false, bucket.ExceededMessage(domain.BucketStock, held, amount, budget)
	}
	return true, ""
}
