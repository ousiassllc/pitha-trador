package shadow

import (
	"database/sql"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// TraderOptions are the jev.Trader options of the application (issue #872): the
// news source, the live prompt version read from runtime_settings (a promotion
// changes it) and the shadow that asks a candidate version about the same
// input. The shadow runs only in Paper and is off until a person turns it on on
// the Settings screen.
func TraderOptions(db *sql.DB, client *jev.Client, settings *system.RuntimeSettingsRepository, news jev.NewsSource) []jev.Option {
	return TraderOptionsWith(db, client, settings, news, func(s *Shadow) jev.ShadowSubmitter { return s })
}

// TraderOptionsWith is TraderOptions with the shadow seam's submitter built from
// the shadow by seam, so that something else (the model-comparison trial, issue
// #1059) can sit next to the shadow without the option list being written twice.
func TraderOptionsWith(db *sql.DB, client *jev.Client, settings *system.RuntimeSettingsRepository, news jev.NewsSource, seam func(*Shadow) jev.ShadowSubmitter) []jev.Option {
	s := New(client, judgement.NewShadowRepository(db), settings, metalabelsettings.RuntimeMode, time.Now)
	return []jev.Option{jev.WithNewsSource(news), jev.WithPromptSettings(settings), jev.WithShadow(seam(s))}
}
