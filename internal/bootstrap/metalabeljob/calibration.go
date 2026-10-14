package metalabeljob

import (
	"context"
	"database/sql"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
	servicemetalabel "github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// CalibrationSource is the Calibration screen's data source (issue #766,
// parent #752): the labeled-outcome metrics (Metrics) plus the newest
// meta-labeling model's OOS report (Report), which the
// internal/web/handler/calibration handler detects by type assertion.
type CalibrationSource struct {
	*calibration.Service
	*servicemetalabel.Reporter
}

// NewCalibrationSource returns metrics extended with the newest model in db,
// reading the LLM cutoff from settings at every call so a Settings change
// shows without a restart.
func NewCalibrationSource(metrics *calibration.Service, db *sql.DB, settings *system.RuntimeSettingsRepository) CalibrationSource {
	load := func(ctx context.Context) (metalabelsettings.Settings, error) {
		return metalabelsettings.Load(ctx, settings)
	}
	return CalibrationSource{Service: metrics, Reporter: servicemetalabel.NewReporter(metalabel.NewRepository(db), load)}
}
