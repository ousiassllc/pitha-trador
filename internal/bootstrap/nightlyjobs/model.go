package nightlyjobs

import (
	"context"
	"database/sql"

	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// ModelTask is the research model's nightly training task (issue #1092).
func ModelTask(_ *sql.DB, _ *system.RuntimeSettingsRepository) maintenance.Task {
	return maintenance.Task{Name: "research_model", Label: "研究用モデルの夜間の学習", Run: func(context.Context) error { return maintenance.ErrSkipped }}
}
