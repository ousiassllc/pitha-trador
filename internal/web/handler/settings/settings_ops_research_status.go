package settings

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/service/researchjobstatus"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings/researchjobpanel"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// ResearchJobStatusSource reads how the research-only nightly tasks are doing
// (*researchjobstatus.Service implements it). Read-only: the panel it feeds is for
// checking the operation and changes nothing.
type ResearchJobStatusSource interface {
	Tasks(ctx context.Context) ([]researchjobstatus.Task, error)
}

// WithResearchJobStatus shows the 夜間タスクの状態 panel in the research-jobs card.
// Without it the card shows its switches only.
func (h *SettingsHandler) WithResearchJobStatus(src ResearchJobStatusSource) *SettingsHandler {
	h.researchJobs = src
	return h
}

// researchJobsDerived is the research-jobs card's read-only status panel. A read
// failure is logged and the panel says so (the switches still render) instead of
// failing the Settings screen or guessing a state.
func (h *SettingsHandler) researchJobsDerived(ctx context.Context) []molecules.SettingDerivedItem {
	if h.researchJobs == nil {
		return nil
	}
	tasks, err := h.researchJobs.Tasks(ctx)
	if err != nil {
		slog.Error("settings: read the research tasks' status; showing the panel as unreadable", "error", err)
		return []molecules.SettingDerivedItem{{Key: "error", Label: "夜間タスクの状態を読み取れませんでした", Off: true}}
	}
	return researchjobpanel.Items(tasks)
}
