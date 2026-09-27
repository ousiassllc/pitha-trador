package risk

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// PositionCloser closes every open position when Kill Switch fires for a
// reason requiring it (docs/architecture/overview.md §10.3's forced-close
// list, functional.md FR-RISK-3). Execution (a later sub-scope; this
// package's own scope note: "実際のクローズ実行はExecution issueと連携する
// インターフェースを定義する") provides the real implementation;
// NoopPositionCloser is the placeholder default.
type PositionCloser interface {
	CloseAll(ctx context.Context, reason string) error
}

// NoopPositionCloser is the placeholder PositionCloser used until
// Execution wires a real implementation in: it records nothing and does
// nothing, since there are no open positions to close yet.
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
}
