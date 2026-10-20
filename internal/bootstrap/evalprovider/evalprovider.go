// Package evalprovider adds the realized-performance evaluation (issue #777)
// to the insight provider the router serves: the Performance page's 実績評価
// section and `GET /api/v1/performance/evaluation` read it through
// actuals.Evaluator.
package evalprovider

import (
	"context"
	"database/sql"

	actualsrepo "github.com/ousiassllc/pitha-trador/internal/repository/actuals"
	"github.com/ousiassllc/pitha-trador/internal/service/actuals"
	"github.com/ousiassllc/pitha-trador/internal/web/insightapi"
)

type provider struct {
	insightapi.Provider
	source  actuals.Source
	capital func(ctx context.Context) float64
}

// New returns p that also evaluates the closed Paper trades in db. capital
// returns the operating capital in force at the time of each evaluation (the
// Risk Engine's initial capital, which the Settings screen can change, issue
// #804; <= 0 when unset): the base of the capital-ratio drawdown and the
// documented 合格ライン.
func New(p insightapi.Provider, db *sql.DB, capital func(ctx context.Context) float64) insightapi.Provider {
	return provider{Provider: p, source: actualsrepo.NewReader(db), capital: capital}
}

// Evaluate implements actuals.Evaluator over the capital in force now.
func (p provider) Evaluate(ctx context.Context, r actuals.Range) (actuals.Evaluation, error) {
	return actuals.New(p.source, p.capital(ctx), actuals.DefaultCriteria()).Evaluate(ctx, r)
}
