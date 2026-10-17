package training

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
)

// Reasons recorded in metalabel_models.reject_reason when the LLM cutoff does
// not support the OOS evaluation (issue #776): the model is stored for the
// record but never adopted, so an existing active model stays in force.
const (
	// RejectLLMCutoffPlaceholder: metalabel.llm_cutoff is unset or still the
	// shipped placeholder, so the OOS window may overlap what the LLM already
	// knows (look-ahead bias).
	RejectLLMCutoffPlaceholder = "llm_cutoff_placeholder"
	// RejectLLMModelChanged: the OOS decisions were answered by a Jev model
	// other than the one the cutoff was confirmed for
	// (metalabel.llm_cutoff_model); the cutoff must be confirmed again.
	RejectLLMModelChanged = "llm_model_changed"
)

// cutoffGate returns the reject reason (with a human-readable detail) when the
// LLM cutoff cannot support adopting a model evaluated on oos, or "" when it
// can. A cutoff whose Jev model is not recorded is only a warning
// (metalabelsettings.CutoffUnverified), so Paper operation is not blocked for
// an operator who set just the date.
func cutoffGate(s metalabelsettings.Settings, oos []Row) (reason, detail string) {
	if s.CutoffStatus() == metalabelsettings.CutoffPlaceholder {
		return RejectLLMCutoffPlaceholder, fmt.Sprintf("metalabel.llm_cutoff is %s (unset or the placeholder default)", s.LLMCutoff)
	}
	if s.LLMCutoffModel == "" {
		return "", ""
	}
	var others []string
	for _, r := range oos {
		if r.JevModelID != "" && r.JevModelID != s.LLMCutoffModel && !slices.Contains(others, r.JevModelID) {
			others = append(others, r.JevModelID)
		}
	}
	if len(others) == 0 {
		return "", ""
	}
	slices.Sort(others)
	return RejectLLMModelChanged, fmt.Sprintf("OOS decisions were answered by %s but the cutoff was confirmed for %q",
		strings.Join(others, ", "), s.LLMCutoffModel)
}

// logCutoffWarning warns, on every nightly run, while the cutoff is a
// placeholder or has no recorded provenance.
func logCutoffWarning(ctx context.Context, s metalabelsettings.Settings) {
	if warning := s.CutoffWarning(); warning != "" {
		slog.WarnContext(ctx, "metalabel: LLM cutoff is not confirmed; "+warning,
			"llm_cutoff", s.LLMCutoff, "cutoff_status", s.CutoffStatus(), "llm_cutoff_model", s.LLMCutoffModel)
	}
}

// fitBeforeCutoff counts the fit and calibration rows decided before the cutoff:
// the model learns from them although the OOS evaluation never uses them (the
// documented decision of FR-META-6; they are only logged).
func fitBeforeCutoff(split Split) int {
	n := 0
	for _, rows := range [][]Row{split.Fit, split.Calibrate} {
		for _, r := range rows {
			if r.DecidedAt.Before(split.LLMCutoff) {
				n++
			}
		}
	}
	return n
}
