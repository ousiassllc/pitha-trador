package dataexport

import (
	"database/sql"

	"github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/decisionsnapshot"
	"github.com/ousiassllc/pitha-trador/internal/repository/decisiontrade"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
)

// NewLocal returns the Service cmd/desktop and cmd/server use (issue #773):
// the trade log and the decision log read from db, the measurement log from
// metrics (the log directory's logging.Exporter). Every collector is
// read-only, and nothing leaves the local ZIP (FR-EXPORT-6).
func NewLocal(db *sql.DB, metrics MetricsSource) *Service {
	signals := trading.NewSignalRepository(db)
	return New(
		Trades{Orders: trading.NewOrderRepository(db), Signals: signals, Closed: decisiontrade.New(db)},
		Decisions{Decisions: judgement.NewDecisionRepository(db), Features: decisionsnapshot.New(db), Signals: signals, Labels: calibration.NewCalibrationRepository(db)},
		Metrics{Source: metrics},
	)
}
