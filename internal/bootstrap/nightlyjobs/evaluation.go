package nightlyjobs

import (
	"context"
	"database/sql"

	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// EvaluationTask is the daily evaluation record's maintenance task (issue #1089).
func EvaluationTask(_ *sql.DB, _ *system.RuntimeSettingsRepository) maintenance.Task {
	return maintenance.Task{Name: "jev_evaluation_daily", Label: "Jev 評価の日次記録", Run: func(context.Context) error { return maintenance.ErrSkipped }}
}
