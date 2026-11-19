// Package researchextrawire collects the optional research parts the bootstrap wires
// in: the J-Quants read adapter (issue #1093), the EDINET document list (issue
// #1096) and the research FX candle record (issue #1084) as scheduler maintenance
// tasks, and the replay evaluation (issue #1085) as a background runner. All are
// off until switched on on the Settings screen and send no request without their
// API key. The 立花 news task is not wired (it waits for #725). Nothing here is an
// input of trading.
package researchextrawire

import (
	"context"
	"database/sql"
	"sync"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/disclosuresrcwire"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/jquantswire"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/replaywire"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/researchfxjob"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

// Wire is the research parts of the composition root.
type Wire struct {
	db       *sql.DB
	settings *system.RuntimeSettingsRepository
	jquants  string // stored JQUANTS_API_KEY ("" = not set); only the J-Quants task reads it
	edinet   string // stored EDINET_API_KEY ("" = not set); only the EDINET task reads it
	replay   *replaywire.Wire
}

// New returns the Wire over db and the database file at dbPath (the replay runner
// opens its own connection there; "" leaves it off). The replay uses the trial's
// OpenRouter settings.
func New(db *sql.DB, settings *system.RuntimeSettingsRepository, secrets config.Secrets, dbPath string) *Wire {
	return &Wire{db: db, settings: settings, jquants: secrets.JQuantsAPIKey, edinet: secrets.EdinetAPIKey,
		replay: replaywire.New(dbPath, secrets)}
}

// Option registers the maintenance tasks (J-Quants, EDINET, research FX) with the scheduler.
func (w *Wire) Option() scheduler.Option {
	return func(s *scheduler.Scheduler) {
		scheduler.WithMaintenanceTask(jquantswire.New(w.db, w.settings, w.jquants).Task())(s)
		scheduler.WithMaintenanceTask(researchfxjob.New(w.db, w.settings).Task())(s)
		for _, task := range disclosuresrcwire.Tasks(w.db, w.settings, w.edinet) {
			scheduler.WithMaintenanceTask(task)(s)
		}
	}
}

// Start launches the replay runner (see replaywire.Wire.Start). A nil Wire does nothing.
func (w *Wire) Start(ctx context.Context, wg *sync.WaitGroup) {
	if w != nil {
		w.replay.Start(ctx, wg)
	}
}
