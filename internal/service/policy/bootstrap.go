package policy

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/paperbootstrap"
)

// WithPaperBootstrap lets Evaluate enter on the legacy threshold rule while no
// meta-labeling model is adopted and src's Settings switch is on (issue #798).
// It applies only when mode is metalabelsettings.ModePaper; in any other mode
// the switch is not even read.
func WithPaperBootstrap(mode metalabelsettings.Mode, src paperbootstrap.Source) Option {
	return func(e *Engine) { e.bootstrap = paperbootstrap.New(mode, src) }
}

// entryRule picks one Evaluate's entry rule and policy_version: the model's
// (meta) or, for a bootstrap entry, the legacy rule (nil) under
// paperbootstrap.Version. Bootstrap needs a model source that answered with no
// active model; a model that could not be loaded keeps the skip.
func (e *Engine) entryRule(ctx context.Context, meta *metaEntry, version string) (*metaEntry, string, bool) {
	if e.bootstrap == nil || meta == nil || meta.model != nil || meta.unavailable != "" || !e.bootstrap.Active(ctx) {
		return meta, version, false
	}
	return nil, paperbootstrap.PolicyVersion(version, MetaVersion), true
}
