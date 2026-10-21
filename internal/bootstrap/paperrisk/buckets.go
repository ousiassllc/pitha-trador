package paperrisk

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/bucket"
)

// InstrumentReader is the part of market.InstrumentRepository BucketProvider reads.
type InstrumentReader interface {
	Get(ctx context.Context, id int64) (domain.Instrument, error)
}

// BucketProvider reads the buckets' budgets and holdings, and an instrument's
// bucket, for the Paper Risk Engine (risk.Buckets). A Live Engine is built
// without one. It lives here, not in package bucket, so that bucket (and the
// backtest's simrisk, which uses it) stays free of repository/trading
// (issue #842).
type BucketProvider struct {
	positions   *trading.PositionRepository
	settings    bucketsettings.Store
	instruments InstrumentReader
}

// Buckets is the Paper Risk Engine's budget gate input (risk.Config.Buckets):
// the budgets in runtime_settings, each bucket's open positions and the
// instruments' asset classes. A Live Engine is built without it.
func Buckets(positions *trading.PositionRepository, settings bucketsettings.Store, instruments InstrumentReader) *BucketProvider {
	return &BucketProvider{positions: positions, settings: settings, instruments: instruments}
}

// BucketOf is the bucket instrumentID's asset class puts its entries in.
func (p *BucketProvider) BucketOf(ctx context.Context, instrumentID int64) (string, error) {
	in, err := p.instruments.Get(ctx, instrumentID)
	if err != nil {
		return "", err
	}
	return bucket.Of(in.AssetClass)
}

// BudgetJPY is bucket's budget in force now for an operating capital of
// capitalJPY (the stored row, else the default split: the stock bucket the
// whole capital, the bond_etf bucket 0).
func (p *BucketProvider) BudgetJPY(ctx context.Context, name string, capitalJPY float64) (float64, error) {
	budgets, err := bucketsettings.Effective(ctx, p.settings, capitalJPY)
	if err != nil {
		return 0, err
	}
	return bucket.BudgetOf(budgets, name), nil
}

// HeldJPY is what bucket holds now: the market value (bucket.Notional) of its
// open positions (positions.bucket).
func (p *BucketProvider) HeldJPY(ctx context.Context, name string) (float64, error) {
	open, err := p.positions.ListOpenByBucket(ctx, name)
	if err != nil {
		return 0, err
	}
	var held float64
	for _, pos := range open {
		held += bucket.Notional(pos.Quantity, pos.EntryPrice, pos.CurrentPrice)
	}
	return held, nil
}
