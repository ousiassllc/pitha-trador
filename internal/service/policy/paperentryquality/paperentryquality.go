// Package paperentryquality gives the Policy Engine a Paper-only minimum entry
// quality (issue #861, FR-POLICY-8): in Paper the engine's thresholds carry
// policy.paper.long|short.min_entry_quality (internal/config/paperpolicysettings,
// default good) in place of policy.long|short.min_entry_quality (default
// strong, the Live values); in any other mode the policy.* values apply
// unchanged and the Paper keys are not even read. Every other threshold is
// shared. It wraps the engine's policy.PolicySource, so the backtest and the
// Execution Engine, which read the RuntimePolicy directly, are unaffected.
package paperentryquality

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/paperpolicysettings"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// Source supplies the Paper minimum entry quality per side;
// paperpolicysettings.Source implements it.
type Source interface {
	PaperEntryQuality(ctx context.Context) (paperpolicysettings.Settings, error)
}

// Policy is a policy.PolicySource: Inner's thresholds, with the Paper minimum
// entry quality applied while Mode is metalabelsettings.ModePaper.
type Policy struct {
	Inner  policy.PolicySource
	Mode   metalabelsettings.Mode
	Source Source
}

// Option is policy.WithPolicySource(inner) with the Paper minimum entry
// quality read from store at every decision (so a change on the Settings screen
// or through the MCP applies to the next decision without a restart), for the
// build's runtime mode (metalabelsettings.RuntimeMode).
func Option(inner policy.PolicySource, store paperpolicysettings.Store) policy.Option {
	return policy.WithPolicySource(Policy{Inner: inner, Mode: metalabelsettings.RuntimeMode, Source: paperpolicysettings.Source{Store: store}})
}

// CurrentThresholds implements policy.PolicySource.
func (p Policy) CurrentThresholds(ctx context.Context) (config.PolicyConfig, error) {
	cfg, err := p.Inner.CurrentThresholds(ctx)
	if err != nil || p.Mode != metalabelsettings.ModePaper {
		return cfg, err
	}
	paper, err := p.Source.PaperEntryQuality(ctx)
	if err != nil {
		return config.PolicyConfig{}, fmt.Errorf("paperentryquality: read the Paper min entry quality: %w", err)
	}
	cfg.Long.MinEntryQuality = paper.LongMinEntryQuality
	cfg.Short.MinEntryQuality = paper.ShortMinEntryQuality
	return cfg, nil
}

// AppliedPolicyVersion implements policy.PolicySource.
func (p Policy) AppliedPolicyVersion(ctx context.Context) (string, error) {
	return p.Inner.AppliedPolicyVersion(ctx)
}
