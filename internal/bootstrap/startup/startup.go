// Package startup holds the process-level startup concerns cmd/desktop and
// cmd/server share: where the logs live, installing the file logger and
// turning run's returned error into an ERROR log record plus an exit code.
package startup

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/logsettings"
	"github.com/ousiassllc/pitha-trador/internal/logging"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// DefaultLogDir is the log directory used while the Settings screen has not
// configured one: "logs" next to the SQLite DB at dbPath (like the instance
// locks), so it never depends on the process's working directory (a
// machine-scope install or an autostarted process may run from an
// unwritable one).
func DefaultLogDir(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "logs")
}

// LogDir returns the absolute directory the daily JSON logs are written to
// (RunMain), the Scheduler's logging.Archiver compresses old files in and
// the error-log logging.Exporter reads (requirements/non-functional.md §5):
// the Settings screen's log directory (runtime_settings config.KeyLogDir,
// issue #708) when one is stored in the database at dbPath, otherwise
// DefaultLogDir. It is read once when the process starts - the logger is
// installed before the database is opened for the application - so a change
// made in Settings applies after the next restart. An unreadable or invalid
// stored value falls back to the default rather than keeping the application
// from logging at all.
func LogDir(dbPath string) string {
	raw, ok, err := sqlitedb.PeekRuntimeSetting(context.Background(), dbPath, config.KeyLogDir)
	if err == nil && ok {
		var dir string
		if json.Unmarshal([]byte(raw), &dir) == nil && filepath.IsAbs(dir) {
			return dir
		}
	}
	return DefaultLogDir(dbPath)
}

// LogLevel returns the minimum level of the daily JSON log: the Settings
// screen's log level (runtime_settings config.KeyLogLevel, issue #772) when
// one is stored in the database at dbPath, otherwise
// logsettings.DefaultLevel (INFO). Like LogDir it is read once when the
// process starts, so a change made in Settings applies after the next
// restart; an unreadable or invalid stored value falls back to the default.
func LogLevel(dbPath string) slog.Level {
	return logsettings.LoadLevel(context.Background(), peekStore{dbPath: dbPath})
}

// peekStore reads runtime_settings straight from the database file, before
// the application has opened the database (sqlitedb.PeekRuntimeSetting).
type peekStore struct{ dbPath string }

func (p peekStore) Get(ctx context.Context, key string) (string, bool, error) {
	return sqlitedb.PeekRuntimeSetting(ctx, p.dbPath, key)
}

// LogSetup is where and at which level the process logs.
type LogSetup struct {
	Dir   string
	Level slog.Level
}

// setupLogging installs the process-wide JSON slog logger writing to the
// rotating daily files in the directory resolve returns - the full log
// (<date>.log) and the ERROR-only log (<date>-error.log) - at the level it
// returns, and returns the function that closes them. A log directory that
// cannot be resolved or created (read-only location, ...) must not stop the
// application from starting: the logger then writes to stderr and says why.
func setupLogging(resolve func() (LogSetup, error)) (closeLog func()) {
	setup, err := resolve()
	if err == nil {
		var rw, ew *logging.RotatingWriter
		if rw, err = logging.NewRotatingWriter(setup.Dir); err == nil {
			if ew, err = logging.NewErrorRotatingWriter(setup.Dir); err == nil {
				slog.SetDefault(logging.NewSplit(rw, ew, setup.Level))
				return func() { _ = rw.Close(); _ = ew.Close() }
			}
			_ = rw.Close()
		}
	}
	slog.SetDefault(logging.New(os.Stderr, setup.Level))
	slog.Warn("startup: file logging unavailable, logging to stderr", "error", err)
	return func() {}
}

// RunMain is the shared body of both entrypoints' main: it sets up logging
// (in the directory and at the level resolve returns), calls run, records a returned error at
// ERROR level and returns the process exit code for os.Exit. log.Fatal
// would log it at INFO through slog.SetDefault, below the error-log
// export's threshold, so the reason the app did not start would be missing
// from the export (issue #547). run's defers (DB close, lock release) have
// already run when the error is logged; the log file is closed last.
func RunMain(name string, resolve func() (LogSetup, error), run func() error) (exitCode int) {
	defer setupLogging(resolve)()
	if err := run(); err != nil {
		slog.Error(name+": fatal error, exiting", "error", err)
		return 1
	}
	return 0
}
