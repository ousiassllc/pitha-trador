package trial

import (
	"database/sql"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/shadow"
)

// TraderOptions are the jev.Trader options of the application: shadow.TraderOptions
// (news source, the live prompt version read from runtime_settings, the Jev
// shadow) with the model-comparison trial next to the shadow on the Trader's single
// shadow seam. The trial runs only in Paper and is off until a person turns it on
// on the Settings screen; keys carries the optional OpenRouter settings (without a
// key no arm is called).
func TraderOptions(db *sql.DB, client *jev.Client, settings *system.RuntimeSettingsRepository, news jev.NewsSource, keys Keys) []jev.Option {
	return shadow.TraderOptionsWith(db, client, settings, news, func(s *shadow.Shadow) jev.ShadowSubmitter {
		t := New(judgement.NewTrialRepository(db), settings, NewCallers(keys), metalabelsettings.RuntimeMode, time.Now)
		return Compose(s, t)
	})
}
