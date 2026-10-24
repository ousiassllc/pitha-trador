package system

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// DailyHaltSource is what the Home screen's 要対応 list needs from
// internal/service/risk.Engine: today's daily entry halt (issue #753,
// FR-RISK-8), nil while none applies.
type DailyHaltSource interface {
	DailyHalt(ctx context.Context) (*domain.DailyEntryHalt, error)
}
