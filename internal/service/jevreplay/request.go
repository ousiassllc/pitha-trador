package jevreplay

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// ErrInvalidRequest is wrapped by every rejected run request; the message is an
// operator-facing reason that names the argument and the rule, never a secret.
var ErrInvalidRequest = errors.New("jevreplay: invalid request")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidRequest}, args...)...)
}

// Request is what a person asks of a run: the live decisions to replay (their
// timestamp in [From, To), at most MaxRows of them, oldest first) and the arms
// to ask (empty: every configured arm).
type Request struct {
	// DecisionType is jevreplay.DecisionTypeTrader (empty means the same).
	DecisionType string
	From, To     time.Time
	MaxRows      int
	Arms         []string
}

// Resolve checks r against the configured arms and returns the spec to run and
// the arms it names, in configuration order (a canonical spec, so the same
// request always makes the same estimate token). A prompt version this build
// does not register is refused here: nothing could be sent to that arm.
func (r Request) Resolve(configured []jevtrialsettings.Arm) (PlanInput, error) {
	if t := r.DecisionType; t != "" && t != jevreplay.DecisionTypeTrader {
		return PlanInput{}, invalid("decision_type must be %q: only trader decisions can be replayed", jevreplay.DecisionTypeTrader)
	}
	if r.MaxRows < 1 || r.MaxRows > jevreplay.MaxRows {
		return PlanInput{}, invalid("max_rows must be 1〜%d", jevreplay.MaxRows)
	}
	if !r.From.Before(r.To) {
		return PlanInput{}, invalid("the period must end after it starts")
	}
	if len(configured) == 0 {
		return PlanInput{}, invalid("no trial arm is configured (jev.trial.arms)")
	}
	for _, id := range r.Arms {
		if !slices.ContainsFunc(configured, func(a jevtrialsettings.Arm) bool { return a.ID == id }) {
			return PlanInput{}, invalid("arm %q is not configured", id)
		}
	}
	if len(slices.Compact(slices.Sorted(slices.Values(r.Arms)))) != len(r.Arms) {
		return PlanInput{}, invalid("an arm is named twice")
	}
	var arms []jevtrialsettings.Arm
	for _, a := range configured {
		if len(r.Arms) > 0 && !slices.Contains(r.Arms, a.ID) {
			continue
		}
		if !jev.IsTraderVersion(a.PromptVersion) {
			return PlanInput{}, invalid("arm %q: prompt version %q is not a registered trader prompt version", a.ID, a.PromptVersion)
		}
		arms = append(arms, a)
	}
	spec := jevreplay.Spec{
		DecisionType: jevreplay.DecisionTypeTrader,
		From:         jevshadowsettings.Day(r.From), To: jevshadowsettings.Day(r.To),
		MaxRows: r.MaxRows, Arms: armIDs(arms),
	}
	return PlanInput{Spec: spec, From: r.From, To: r.To, Arms: arms}, nil
}
