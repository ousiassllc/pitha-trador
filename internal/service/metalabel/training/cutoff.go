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
	// RejectLLMCutoffUnverified: Live only. The cutoff is a real date, but the
	// Jev model it was confirmed for is not recorded, so a model switch could
	// not be detected; Paper adopts with a warning instead.
	RejectLLMCutoffUnverified = "llm_cutoff_unverified"
)

// cutoffGate returns the reject reason (with a human-readable detail) when the
// LLM cutoff cannot support adopting a model evaluated on oos in mode, or ""
// when it can. A cutoff whose Jev model is not recorded
// (metalabelsettings.CutoffUnverified) is only a warning in Paper, so Paper
// operation is not blocked for an operator who set just the date, but it
// stops the adoption before Live (issue #780, decided 2026-10-19).
func cutoffGate(s metalabelsettings.Settings, mode metalabelsettings.Mode, oos []Row) (reason, detail string) {
	switch s.CutoffStatus() {
	case metalabelsettings.CutoffPlaceholder:
		return RejectLLMCutoffPlaceholder, fmt.Sprintf("metalabel.llm_cutoff is %s (unset or the placeholder default)", s.LLMCutoff)
	case metalabelsettings.CutoffUnverified:
		if mode == metalabelsettings.ModeLive {
			return RejectLLMCutoffUnverified, fmt.Sprintf("metalabel.llm_cutoff %s has no recorded Jev model (%s); Live needs it", s.LLMCutoff, metalabelsettings.KeyLLMCutoffModel)
		}
		return "", ""
	}
	var others []string
	for _, r := range oos {
		if r.JevModelID != "" && !CutoffModelMatches(s.LLMCutoffModel, r.JevModelID) && !slices.Contains(others, r.JevModelID) {
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

// The values of CutoffModelStatus.
const (
	// CutoffModelMatch: the Jev model is the one the cutoff was confirmed for.
	CutoffModelMatch = "match"
	// CutoffModelMismatch: the Jev model differs, so the cutoff must be
	// confirmed again (RejectLLMModelChanged).
	CutoffModelMismatch = "mismatch"
	// CutoffModelUnset: there is nothing to compare: no cutoff model is
	// recorded (metalabel.llm_cutoff_model is empty) or no Jev model is known.
	CutoffModelUnset = "unset"
)

// CutoffModelMatches reports whether jevModelID is the Jev model the LLM
// cutoff was confirmed for: an exact string match, no trimming or case
// folding, so "jev-1.13" and "jev-1.13.0" differ. cutoffGate and the Paper
// results MCP's jev_models tool share it.
func CutoffModelMatches(cutoffModel, jevModelID string) bool { return cutoffModel == jevModelID }

// CutoffModelStatus compares the recorded cutoff model with the Jev model that
// answered last: CutoffModelUnset when either is empty (nothing to compare),
// otherwise CutoffModelMatch or CutoffModelMismatch by CutoffModelMatches.
func CutoffModelStatus(cutoffModel, jevModelID string) string {
	switch {
	case cutoffModel == "" || jevModelID == "":
		return CutoffModelUnset
	case CutoffModelMatches(cutoffModel, jevModelID):
		return CutoffModelMatch
	}
	return CutoffModelMismatch
}
