package shadow

import (
	"database/sql"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// ForApp is the application's Shadow: it stores answers in jev_shadow_decisions
// (judgement.ShadowRepository), reads its settings from runtime_settings and runs
// only in Paper (metalabelsettings.RuntimeMode). Exported so the trial can sit
// next to it on the Trader's shadow seam (internal/service/jev/trial).
func ForApp(db *sql.DB, client *jev.Client, settings *system.RuntimeSettingsRepository) *Shadow {
	return New(client, judgement.NewShadowRepository(db), settings, metalabelsettings.RuntimeMode, time.Now)
}

// TraderOptions are the jev.Trader options of the application (issue #872): the
// news source, the live prompt version read from runtime_settings (a promotion
// changes it) and the shadow that asks a candidate version about the same
// input. The shadow runs only in Paper and is off until a person turns it on on
// the Settings screen.
func TraderOptions(db *sql.DB, client *jev.Client, settings *system.RuntimeSettingsRepository, news jev.NewsSource) []jev.Option {
	return []jev.Option{jev.WithNewsSource(news), jev.WithPromptSettings(settings), jev.WithShadow(ForApp(db, client, settings))}
}
