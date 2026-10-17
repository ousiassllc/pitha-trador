// Package exitalignjob is the composition-root glue of the Calibration
// screen's label-versus-actual-exit view (issue #774): it extends
// metalabeljob.CalibrationSource with exitalign.Reader built over the same
// database, so `GET /api/v1/calibration/exit-alignment` can compare the
// Settings label/cost basis with the Paper Execution Engine's exit rules.
package exitalignjob

import (
	"context"
	"database/sql"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/metalabeljob"
	"github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	servicecalibration "github.com/ousiassllc/pitha-trador/internal/service/calibration"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/exitalign"
)

// CalibrationSource is metalabeljob.CalibrationSource (metrics and the newest
// model's OOS report) plus the exit-alignment view, which the
// internal/web/handler/calibration handler detects by type assertion.
type CalibrationSource struct {
	metalabeljob.CalibrationSource
	reader *exitalign.Reader
}

// NewCalibrationSource returns metrics extended with the newest model in db and
// the alignment of the Settings label/cost basis with the actual exit rules
// exit, all read from db and settings at every call.
func NewCalibrationSource(metrics *servicecalibration.Service, db *sql.DB, settings *system.RuntimeSettingsRepository, exit execution.Config) CalibrationSource {
	base := metalabeljob.NewCalibrationSource(metrics, db, settings)
	positions := trading.NewPositionRepository(db)
	replayer := exitalign.NewReplayer(judgement.NewDecisionRepository(db), market.NewSnapshotRepository(db), calibration.NewCalibrationRepository(db), settings, positions)
	rule := exitalign.ExitConfig{
		StopLossPct: exit.StopLossPct, TakeProfitPct: exit.TakeProfitPct,
		TrailingStopPct: exit.TrailingStopPct, MaxHoldingMinutes: exit.MaxHoldingMinutes,
	}
	return CalibrationSource{CalibrationSource: base, reader: exitalign.NewReader(settings, rule, replayer, base.Reporter, positions)}
}

// Alignment implements the calibration handler's AlignmentSource.
func (s CalibrationSource) Alignment(ctx context.Context, window time.Duration) (exitalign.LabelExitAlignment, error) {
	return s.reader.Alignment(ctx, window)
}
