package simrisk

import (
	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/bucket"
)

// SetAssetClass records instrumentID's asset_class, which puts its entries and
// positions in that class's budget bucket (issue #833), like live's
// instruments.asset_class. An instrument never recorded is a stock. It is an
// error for an unknown asset class.
func (p *Portfolio) SetAssetClass(instrumentID int64, assetClass string) error {
	name, err := bucket.Of(assetClass)
	if err != nil {
		return err
	}
	p.buckets[instrumentID] = name
	return nil
}

// bucketOf is instrumentID's budget bucket.
func (p *Portfolio) bucketOf(instrumentID int64) string {
	if name, ok := p.buckets[instrumentID]; ok {
		return name
	}
	return domain.BucketStock
}

// budget is bucketName's budget in force: the configured one, else the default
// split (the whole initial capital for stock, 0 for bond_etf).
func (p *Portfolio) budget(bucketName string) float64 {
	overrides := bucketsettings.Overrides{StockBudgetJPY: p.cfg.StockBudgetJPY, BondETFBudgetJPY: p.cfg.BondETFBudgetJPY}
	return bucket.BudgetOf(overrides.Resolve(p.cfg.Limits.InitialCapital), bucketName)
}

// held is live's HeldJPY: the market value of bucketName's open positions,
// measured like the exposure limits.
func (p *Portfolio) held(bucketName string) float64 {
	var held float64
	for _, pos := range p.open {
		if pos.bucket == bucketName {
			held += pos.notional()
		}
	}
	return held
}
