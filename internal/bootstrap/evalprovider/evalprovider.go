// Package evalprovider adds the realized-performance evaluation (issue #777)
// to the insight provider the router serves: the Performance page's 実績評価
// section and `GET /api/v1/performance/evaluation` read it through
// actuals.Evaluator.
package evalprovider

import (
	"database/sql"

	actualsrepo "github.com/ousiassllc/pitha-trador/internal/repository/actuals"
	"github.com/ousiassllc/pitha-trador/internal/service/actuals"
	"github.com/ousiassllc/pitha-trador/internal/web/insightapi"
)

type provider struct {
	insightapi.Provider
	actuals.Evaluator
}

// New returns p that also evaluates the closed Paper trades in db, with
// initialCapital (the risk config's initial_capital; <= 0 when unset) as the
// base of the capital-ratio drawdown and the documented 合格ライン.
func New(p insightapi.Provider, db *sql.DB, initialCapital float64) insightapi.Provider {
	return provider{
		Provider:  p,
		Evaluator: actuals.New(actualsrepo.NewReader(db), initialCapital, actuals.DefaultCriteria()),
	}
}
