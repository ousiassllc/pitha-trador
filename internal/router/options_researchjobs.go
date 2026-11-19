package router

import "github.com/ousiassllc/pitha-trador/internal/web/handler/settings"

// WithResearchJobStatus adds the 夜間タスクの状態 panel (last success and failure of the research-only
// nightly tasks) to the research-jobs card of the Settings screen's 運用設定 section. It has an effect only
// together with WithOperationalSettings; without it the card shows its switches only.
func WithResearchJobStatus(src settings.ResearchJobStatusSource) Option {
	return func(o *options) { o.researchJobStatus = src }
}
