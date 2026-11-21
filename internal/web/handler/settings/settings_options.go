package settings

// Builder options that switch on the optional cards of the Settings screen.

// WithMCPSettings shows the MCP card (modal `modal-mcp`) on the Settings
// screen; call it only when the MCP panel routes are registered.
func (h *SettingsHandler) WithMCPSettings() *SettingsHandler {
	h.mcp = true
	return h
}

// WithMCPExperiments shows the 調整の実験 card (modal `modal-mcp-experiments`)
// on the Settings screen; call it only when the experiment routes are
// registered (the MCP write gate is wired).
func (h *SettingsHandler) WithMCPExperiments() *SettingsHandler {
	h.mcpExperiments = true
	return h
}

// WithJevPromptSettings shows the Jev prompt card (modal `modal-jev-prompt`)
// on the Settings screen; call it only when the panel routes are registered.
func (h *SettingsHandler) WithJevPromptSettings() *SettingsHandler {
	h.jevPrompt = true
	return h
}

// WithJevTrialSettings shows the model-comparison trial card (modal
// `modal-jev-trial`) on the Settings screen; call it only when the panel routes
// are registered.
func (h *SettingsHandler) WithJevTrialSettings() *SettingsHandler {
	h.jevTrial = true
	return h
}

// WithAIRoleSettings shows the 役割ごとのモデル card (modal `modal-ai-roles`) on
// the Settings screen, preceded by the AI provider connections; call it only
// when the panel routes are registered.
func (h *SettingsHandler) WithAIRoleSettings() *SettingsHandler {
	h.aiRoles = true
	return h
}
