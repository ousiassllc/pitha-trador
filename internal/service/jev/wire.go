package jev

import (
	"database/sql"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
)

// TraderOptions are the Trader options of the application (issue #872): the
// news source, the live prompt version read from runtime_settings (a promotion
// changes it) and the shadow that asks a candidate version about the same
// input. The shadow runs only in Paper and is off until a person turns it on on
// the Settings screen.
func TraderOptions(db *sql.DB, client *Client, settings *system.RuntimeSettingsRepository, news NewsSource) []Option {
	shadow := NewShadow(client, judgement.NewShadowRepository(db), settings, metalabelsettings.RuntimeMode, time.Now)
	return []Option{WithNewsSource(news), WithPromptSettings(settings), WithShadow(shadow)}
}
