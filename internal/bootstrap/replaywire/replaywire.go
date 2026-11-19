// Package replaywire wires the replay evaluation (issue #1085) into the
// composition root. It is split out of internal/bootstrap to keep that package
// within its size limit.
//
// The replay sends saved Jev inputs to the trial's arms after the fact and
// records the answers in its own research tables (internal/service/jevreplay).
// It is OFF until a person turns jev.replay.enabled on on the Settings screen;
// with the switch off, no key or no arm the runner makes no external request.
// The runner uses its own connection to the database (sidecar.Open), so that
// nothing it does reaches the DB-write failure streak the Risk Engine reads,
// and it runs in Paper only. Its results are never an input of trading.
package replaywire

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb/sidecar"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
	service "github.com/ousiassllc/pitha-trador/internal/service/jevreplay"
)

// Wire is the replay's background part. The zero value (and nil) does nothing.
type Wire struct {
	dbPath string
	keys   trial.Keys
	opts   []service.Option // tests
}

// New returns the Wire over the database file at dbPath and the trial's
// OpenRouter settings (the replay uses the same key as the trial, never another
// secret). Without a database path the Wire does nothing.
func New(dbPath string, secrets config.Secrets) *Wire {
	if dbPath == "" {
		return nil
	}
	return &Wire{dbPath: dbPath, keys: trial.Keys{APIKey: secrets.OpenRouterAPIKey, BaseURL: secrets.OpenRouterBaseURL}}
}

// Start opens the runner's database connection and launches it on wg; it ends
// with ctx (in-flight calls are cancelled and a cut-off run continues at the next
// start). A connection that cannot be opened only switches the replay off
// (logged). Start returns at once and makes no request: the runner reads the
// switch on every pass and does nothing while it is off.
func (w *Wire) Start(ctx context.Context, wg *sync.WaitGroup) {
	if w == nil {
		return
	}
	conn, err := sidecar.Open(w.dbPath)
	if err != nil {
		slog.Warn("bootstrap: replay evaluation disabled: cannot open its database connection", "error", err)
		return
	}
	runner := newRunner(conn, w.keys, w.opts...)
	runner.Start()
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		runner.Close()
		if err := conn.Close(); err != nil {
			slog.Warn("bootstrap: closing the replay database connection failed", "error", err)
		}
	}()
	slog.Info("bootstrap: replay evaluation ready (off until it is switched on in Settings; record only, never affects trading)")
}

// newRunner builds the runner over conn: its replay tables, the trial's rows for
// the shared daily caps, the settings (read only) and the trial's per-arm
// clients and key.
func newRunner(conn *sql.DB, keys trial.Keys, opts ...service.Option) *service.Runner {
	callers := trial.NewCallers(keys)
	asReplayCaller := func(arm jevtrialsettings.Arm) service.Caller {
		c, ok := callers(arm).(service.Caller) // a nil client, or one that cannot replay, is never called
		if !ok {
			return nil
		}
		return c
	}
	opts = append([]service.Option{service.WithSecrets(keys.APIKey)}, opts...)
	return service.New(jevreplay.New(conn), judgement.NewTrialRepository(conn), system.NewRuntimeSettingsRepository(conn),
		asReplayCaller, metalabelsettings.RuntimeMode, time.Now, opts...)
}
