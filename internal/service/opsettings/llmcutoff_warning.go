package opsettings

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
)

// llmCutoffWarning is the Settings screen's warning on the LLM cutoff row
// (issue #776): the cutoff is still the placeholder, or a real one whose
// provenance (the Jev model it was confirmed for) is not recorded. The empty
// string means nothing to warn about, or that the settings could not be read
// (logged; the row's own read already reports store failures).
func (s *Service) llmCutoffWarning(ctx context.Context) string {
	settings, err := metalabelsettings.Load(ctx, s.store)
	if err != nil {
		slog.Warn("opsettings: read meta-labeling settings for the LLM cutoff warning", "error", err)
		return ""
	}
	return settings.CutoffWarning()
}
