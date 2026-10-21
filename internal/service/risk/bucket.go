package risk

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/service/risk/bucket"
)

// Budget-check rejection reasons of a new entry (issues #824 and #833,
// internal/service/risk/bucket).
const (
	ReasonBucketDisabled       = bucket.ReasonDisabled
	ReasonBucketBudgetExceeded = bucket.ReasonBudgetExceeded
)

// Buckets supplies the Paper budget buckets (stock, bond_etf) for Check's
// budget gates. paperrisk.BucketProvider (bootstrap) is the production implementation. An Engine
// built without one (every Live Engine) has no budget gate: Live keeps one
// whole-account bucket.
type Buckets interface {
	// BucketOf is the bucket instrumentID's asset class puts its entries in.
	BucketOf(ctx context.Context, instrumentID int64) (string, error)
	// BudgetJPY is bucket's budget in force now, for an operating capital of
	// capitalJPY.
	BudgetJPY(ctx context.Context, bucket string, capitalJPY float64) (float64, error)
	// HeldJPY is what bucket holds now: the market value of its open
	// positions (bucket.Notional, as the exposure limits measure).
	HeldJPY(ctx context.Context, bucket string) (float64, error)
}

// entryBucket is the bucket an entry is judged against and its budget.
type entryBucket struct {
	name   string
	budget float64
}

// gateBucket is Check's first budget gate: it finds the candidate's bucket
// from its asset_class, reads that bucket's budget and rejects the entry while
// it is 0 (the bucket is not used). The bucket is returned for
// checkBucketBudget. Without Buckets it passes.
func (e *Engine) gateBucket(ctx context.Context, instrumentID int64, capitalJPY float64) (b entryBucket, ok bool, reason string) {
	if e.buckets == nil {
		return b, true, ""
	}
	name, err := e.buckets.BucketOf(ctx, instrumentID)
	if err != nil {
		ok, reason = e.failClosed("read instrument bucket", err)
		return b, ok, reason
	}
	budget, err := e.buckets.BudgetJPY(ctx, name, capitalJPY)
	if err != nil {
		ok, reason = e.failClosed("read bucket budget", err)
		return b, ok, reason
	}
	b = entryBucket{name: name, budget: budget}
	if bucket.Disabled(budget) {
		return b, false, bucket.DisabledMessage(name, budget)
	}
	return b, true, ""
}

// checkBucketBudget is Check's second budget gate, run once the entry's size
// is known: what b holds plus this entry (qty at price) may not exceed its
// budget. It passes without Buckets.
func (e *Engine) checkBucketBudget(ctx context.Context, b entryBucket, qty int64, price float64) (bool, string) {
	if e.buckets == nil {
		return true, ""
	}
	held, err := e.buckets.HeldJPY(ctx, b.name)
	if err != nil {
		return e.failClosed("read bucket holdings", err)
	}
	if amount := float64(qty) * price; bucket.Exceeded(held, amount, b.budget) {
		return false, bucket.ExceededMessage(b.name, held, amount, b.budget)
	}
	return true, ""
}
