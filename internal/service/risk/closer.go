package risk

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// PositionCloser closes every open position when Kill Switch fires for a
// reason requiring it (docs/architecture/overview.md §10.3's forced-close
// list, functional.md FR-RISK-3). internal/service/execution.Engine is
// the real implementation internal/bootstrap wires in; NoopPositionCloser
// is NewEngine's default when Config.Closer is nil.
type PositionCloser interface {
	CloseAll(ctx context.Context, reason string) error
}

// NoopPositionCloser is NewEngine's default PositionCloser when
// Config.Closer is nil (tests, or a caller with no Execution engine): it
// records nothing and does nothing.
type NoopPositionCloser struct{}

func (NoopPositionCloser) CloseAll(context.Context, string) error { return nil }

// forceCloseReasons is the docs/architecture/overview.md §10.3
// "opt reasonが..." set: Kill Switch reasons that also force-close held
// positions, as opposed to only stopping new entries.
var forceCloseReasons = map[string]bool{
	domain.KillReasonDailyLossLimit:     true,
	domain.KillReasonUnexpectedPosition: true,
	domain.KillReasonFillDiscrepancy:    true,
	domain.KillReasonConsecutiveLosses:  true,
	domain.KillReasonDBWriteFailure:     true,
	domain.KillReasonBrokerAPIError:     true,
	domain.KillReasonOperatorManual:     true,
}
