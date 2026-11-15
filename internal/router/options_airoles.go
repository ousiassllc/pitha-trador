package router

import "github.com/ousiassllc/pitha-trador/internal/web/handler/airoles"

// WithAIRoleSettings enables the Settings screen's 役割ごとのモデル card (`modal-ai-roles`: each AI role's
// provider and model, issue #1080) and its `GET /settings/ai-roles-panel`, `POST /settings/ai-roles/:role`
// and `DELETE /settings/ai-roles/:role` routes; without it the card is hidden and the routes answer 404.
// They are the only way a role's provider or model, the live Jev role's included, can be changed.
func WithAIRoleSettings(svc airoles.Service) Option {
	return func(o *options) { o.aiRoles = svc }
}
