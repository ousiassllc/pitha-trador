package execution

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// EntryThresholdSource supplies the currently-active policy.* entry
// thresholds (config/strategy.yaml < PITHA_POLICY_* env < runtime_settings,
// FR-POLICY-4). internal/service/selfimprove.RuntimePolicy implements it;
// the composition root injects the same instance Policy Engine reads, so
// the continuation_probability低下 exit follows the entry threshold the
// position was admitted under.
type EntryThresholdSource interface {
	CurrentThresholds(ctx context.Context) (config.PolicyConfig, error)
}

// continuationProbDrop reports whether continuation_probability has
// dropped far enough to trigger FR-EXIT-1's continuation_probability低下
// exit for a position on side. The threshold is
// min(Config.MinContinuationProbability, the entry threshold of that side)
// (FR-EXIT-2), so lowering the entry threshold never makes a position
// entered at the new level exit on its very first evaluation, while a
// stricter entry threshold leaves the exit at Config's value.
func (e *Engine) continuationProbDrop(ctx context.Context, side string, continuationProbability float64) bool {
	limit := e.cfg.MinContinuationProbability
	if continuationProbability >= limit {
		return false // not below even the unadjusted limit: the entry lookup could only lower it
	}
	if e.entryThresholds == nil {
		return true
	}
	current, err := e.entryThresholds.CurrentThresholds(ctx)
	if err != nil {
		// Exit management must not stop (FR-EXIT-3): fall back to the
		// configured limit rather than failing the evaluation.
		slog.WarnContext(ctx, "execution: read entry thresholds for continuation exit", "side", side, "error", err)
		return true
	}
	entry := current.Short.MinContinuationProbability
	if side == domain.PositionSideLong {
		entry = current.Long.MinContinuationProbability
	}
	return continuationProbability < min(limit, entry)
}
