package system

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// HealthAlertSource is what the Home screen's 要対応 list needs from
// internal/service/healthalert.Detector: the alerts active now (issue #802),
// none while the market-data pipeline is healthy or outside the watched hours.
type HealthAlertSource interface {
	Active(ctx context.Context) ([]domain.HealthAlert, error)
}
