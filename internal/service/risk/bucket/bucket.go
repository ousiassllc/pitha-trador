// Package bucket is the Paper budget check of a new entry (issue #824, parent
// #823): the stock bucket may hold at most its budget (internal/config/
// bucketsettings), and a bucket whose budget is 0 takes no new entry. The
// live Risk Engine (internal/service/risk) and the backtest's simulated one
// (internal/service/backtest/simrisk) both reject through Disabled/Exceeded,
// so the two keep the same reasons and the same boundary. It is separate from
// package risk only to keep that directory within linterly's size limit.
//
// What a bucket holds is valued at the market, with the same Notional the
// exposure limits use, so the two never disagree on a position's value.
//
// Only entries are checked; an Exit is never held back by a budget.
package bucket

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
)

// Fixed rejection codes of a new entry (they are also the risk.Reason*
// values).
const (
	// ReasonDisabled: the bucket's budget is 0, i.e. the bucket is not used.
	ReasonDisabled = "bucket_disabled"
	// ReasonBudgetExceeded: what the bucket holds plus this entry would exceed its budget.
	ReasonBudgetExceeded = "bucket_budget_exceeded"
)

// Notional is an open position's current market value in yen: quantity at the
// latest mark, or at the entry price until the first mark arrives (currentPrice
// <= 0). The live exposure limits (repoportfolio), the backtest's
// (simrisk) and the stock bucket's holdings all measure with it.
func Notional(quantity int64, entryPrice, currentPrice float64) float64 {
	price := currentPrice
	if price <= 0 {
		price = entryPrice
	}
	return float64(quantity) * price
}

// Disabled reports whether a bucket with budgetJPY takes no new entry.
func Disabled(budgetJPY float64) bool { return budgetJPY <= 0 }

// Exceeded reports whether holding heldJPY and adding amountJPY would go over
// budgetJPY. Reaching the budget exactly is allowed.
func Exceeded(heldJPY, amountJPY, budgetJPY float64) bool { return heldJPY+amountJPY > budgetJPY }

// DisabledMessage is the rejection text of Disabled: the code and the facts.
func DisabledMessage(bucket string, budgetJPY float64) string {
	return fmt.Sprintf("%s: bucket=%s budget_jpy=%.0f", ReasonDisabled, bucket, budgetJPY)
}

// ExceededMessage is the rejection text of Exceeded: the code and the facts.
func ExceededMessage(bucket string, heldJPY, amountJPY, budgetJPY float64) string {
	return fmt.Sprintf("%s: bucket=%s held_jpy=%.0f entry_jpy=%.0f budget_jpy=%.0f", ReasonBudgetExceeded, bucket, heldJPY, amountJPY, budgetJPY)
}

// Provider reads the stock bucket's budget and holdings for the Paper Risk
// Engine (risk.StockBucket). A Live Engine is built without one.
type Provider struct {
	positions *trading.PositionRepository
	settings  bucketsettings.Store
}

// NewProvider returns a Provider over the positions table and the
// runtime_settings the budgets are stored in.
func NewProvider(positions *trading.PositionRepository, settings bucketsettings.Store) *Provider {
	return &Provider{positions: positions, settings: settings}
}

// StockBudgetJPY is the stock bucket's budget in force now for an operating
// capital of capitalJPY (the stored row, else the whole capital).
func (p *Provider) StockBudgetJPY(ctx context.Context, capitalJPY float64) (float64, error) {
	budgets, err := bucketsettings.Effective(ctx, p.settings, capitalJPY)
	if err != nil {
		return 0, err
	}
	return budgets.StockJPY, nil
}

// StockHeldJPY is what the stock bucket holds now: the market value
// (Notional) of its open positions.
func (p *Provider) StockHeldJPY(ctx context.Context) (float64, error) {
	open, err := p.positions.ListOpenByBucket(ctx, domain.BucketStock)
	if err != nil {
		return 0, err
	}
	var held float64
	for _, pos := range open {
		held += Notional(pos.Quantity, pos.EntryPrice, pos.CurrentPrice)
	}
	return held, nil
}
