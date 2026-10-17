package policy

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// DecideReplay is the backtest replay of the live entry rule (issue #775):
// Evaluate without persistence, prediction storage or logging. It runs the same
// decision tree as the live path - the hard guards, the primary side, the
// meta-labeling model's take/skip verdict on its calibrated expected value and
// minimum EV (cfg), then the Risk Engine's gate - with the model the caller
// says was in force at the decision time instead of the newest stored one.
//
// model is that model (nil: none had been trained and activated yet, which
// skips the entry exactly as live does, ReasonMetaSkip with no_active_model);
// modelErr is non-nil when the model in force could not be loaded or parsed,
// which skips the entry as ReasonMetaModelUnavailable (live's behaviour for an
// unusable model). The returned verdict is the model's answer when the
// candidate reached it, nil otherwise.
//
// The signal's PolicyVersion is MetaVersion. Decide, the legacy confidence
// threshold rule, remains only as an explicitly named comparison mode.
func (e *Engine) DecideReplay(ctx context.Context, in Input, model *metalabel.Model, modelErr error, cfg metalabel.DecideConfig) (domain.TradeSignal, *metalabel.Decision) {
	entry := &metaEntry{model: model, cfg: cfg}
	if modelErr != nil {
		entry.unavailable = fmt.Sprintf("model: %v", modelErr)
	}
	sig, detail := e.decide(ctx, in, e.thresholds, entry, MetaVersion)
	return sig, detail.verdict
}
