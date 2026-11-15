package backtestsource

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/paperpolicysettings"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/paperentryquality"
)

// ReplayMode says which minimum entry quality a replay judges a candidate by
// (issue #874): the Paper Policy Engine uses policy.paper.*.min_entry_quality
// (default good, issue #861) while the policy.*.min_entry_quality of the
// RuntimePolicy (default strong) is the Live value, and the replay must say
// which of the two it reproduces. The zero value, and every caller that does
// not choose, is ReplayLive: the replay reads the Live value exactly as it did
// before the Paper mode existed, so those results do not change. It affects
// only what a replay reads; the live Policy Engine decision is untouched.
type ReplayMode string

const (
	// ReplayLive replays with policy.long|short.min_entry_quality (the
	// RuntimePolicy's value: the Live thresholds).
	ReplayLive ReplayMode = "live"
	// ReplayPaper replays with policy.paper.long|short.min_entry_quality, the
	// value the Paper Policy Engine decides with, so a candidate that passes
	// under Paper's good passes in the backtest and in the proposal evaluation.
	ReplayPaper ReplayMode = "paper"
)

// ParseReplayMode parses a flag/option text ("" is ReplayLive).
func ParseReplayMode(s string) (ReplayMode, error) {
	switch m := ReplayMode(s); m {
	case "", ReplayLive:
		return ReplayLive, nil
	case ReplayPaper:
		return m, nil
	}
	return "", fmt.Errorf("backtestsource: replay mode %q must be %q or %q", s, ReplayLive, ReplayPaper)
}

// WithMode returns r replaying in mode.
func (r Replay) WithMode(mode ReplayMode) Replay {
	r.Mode = mode
	return r
}

// Mode reports the replay mode the Source replays in.
func (b *Source) Mode() ReplayMode {
	if b.replay.Mode == "" {
		return ReplayLive
	}
	return b.replay.Mode
}

// ReplayPolicy returns cfg as the replay judges by it: unchanged in ReplayLive,
// with the Paper minimum entry quality in place of the Live one in ReplayPaper.
// ForEachRunConfig applies it to the baseline thresholds, and
// selfimprove.Governor to a proposal's candidate thresholds
// (selfimprove.ReplayPolicy), so both passes of a proposal evaluation use the
// same mode.
func (b *Source) ReplayPolicy(ctx context.Context, cfg config.PolicyConfig) (config.PolicyConfig, error) {
	switch b.Mode() {
	case ReplayLive:
		return cfg, nil
	case ReplayPaper:
		if b.replay.MetaSettings == nil {
			return config.PolicyConfig{}, fmt.Errorf("backtestsource: the %q replay needs the runtime settings the Paper min_entry_quality is read from", ReplayPaper)
		}
		return paperentryquality.Overlay(ctx, paperpolicysettings.Source{Store: b.replay.MetaSettings}, cfg)
	}
	_, err := ParseReplayMode(string(b.replay.Mode))
	return config.PolicyConfig{}, err
}
