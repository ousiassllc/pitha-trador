// Package bootstrap builds the shared startup state - an open, migrated
// SQLite *sql.DB plus the parsed config/strategy.yaml and config/risk.yaml
// - that both cmd/desktop and cmd/server need before internal/router.New
// can wire real service implementations in (docs/architecture/overview.md
// §10.1 起動時フロー's "マイグレーション適用確認・接続初期化"/config
// load step). Neither cmd/ entrypoint performed this before issue #42;
// this package exists so the DB-open/migrate and YAML-load logic lives in
// exactly one place shared by both (issue #41's "実配線" root files).
//
// Later sub-scopes (issue #41's other split children, #44 onward) extend
// this package as each real service (Market Data, Jev, Policy, Risk,
// Execution, ...) gains its own composition-root wiring step; this first
// scope only covers "DB接続・設定値が使える状態" (issue #42's own
// non-scope note - it does not construct any of those services itself).
package bootstrap

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// Env* are the environment variables that override the default DB/config
// file locations below (issue #42's "環境変数での上書き" decision).
// Unlike internal/config's own PITHA_POLICY_*/PITHA_SECRETS-style
// variables (which override individual config *values*), these three
// override *file paths* - useful for a production deployment that keeps
// its SQLite file and edited config/*.yaml copies outside the installed
// application directory.
const (
	EnvDBPath       = "PITHA_DB_PATH"
	EnvStrategyPath = "PITHA_STRATEGY_PATH"
	EnvRiskPath     = "PITHA_RISK_PATH"
)

// repoRoot is this source file's repository root (internal/bootstrap/../..),
// resolved relative to the source file itself rather than the process's
// current working directory - the same precedent internal/router.go's
// staticDir documents (its own doc comment: "regardless of whether the
// caller is `go test`, `cmd/server`, or `wails dev`, each has a different
// cwd"). Once `wails build` packages a single .exe, this - like
// staticDir - needs revisiting (e.g. an executable-relative path via
// os.Executable, or embedding config/*.yaml as a compiled-in default);
// this build only targets `wails dev`/`go run`/`go test` so far.
var repoRoot = func() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}()

// DefaultDBPath returns the conventional per-user SQLite database file
// location: os.UserConfigDir()'s "pitha-trador/pitha.db" subpath. On
// Windows (the production target, requirements/non-functional.md §1)
// this resolves to "%AppData%\pitha-trador\pitha.db", matching
// docs/architecture/er.md §型・規約 exactly; on Linux/macOS dev machines
// it resolves under $XDG_CONFIG_HOME (or the platform's ~/.config
// equivalent).
func DefaultDBPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("bootstrap: resolve user config dir: %w", err)
	}
	return filepath.Join(dir, "pitha-trador", "pitha.db"), nil
}

// Config selects the DB/config file paths Run opens. Every field left
// empty falls back through the corresponding Env* environment variable
// to its documented default (DefaultDBPath, config.DefaultStrategyPath,
// config.DefaultRiskPath).
type Config struct {
	DBPath       string
	StrategyPath string
	RiskPath     string
}

// ResolvedPaths is the concrete (DBPath, StrategyPath, RiskPath) triple
// Run actually used, once Config's fields/Env* overrides/defaults are all
// resolved - exposed on State for startup logging/diagnostics.
type ResolvedPaths struct {
	DBPath       string
	StrategyPath string
	RiskPath     string
}

// State is the "DB接続・設定値が使える状態" issue #42 sets up, shared by
// cmd/desktop and cmd/server so neither entrypoint duplicates DB-open/
// config-load logic. Later sub-scopes build each real service on top of
// these fields; Close releases the DB handle at shutdown.
type State struct {
	DB       *sql.DB
	Strategy *config.StrategyConfig
	Risk     *config.RiskConfig
	Paths    ResolvedPaths
}

// Run opens (creating + migrating if necessary) the SQLite database and
// loads config/strategy.yaml + config/risk.yaml, applying cfg's path
// overrides (falling back to the Env* environment variables, in turn
// falling back to DefaultDBPath/config.DefaultStrategyPath/
// config.DefaultRiskPath resolved under repoRoot).
//
// Callers (cmd/desktop, cmd/server) treat any returned error as fatal: an
// unopenable DB or unparsable config file leaves the process with no
// usable state to serve requests from, so both entrypoints log the error
// and exit rather than attempt to run degraded (issue #42's
// error-handling decision - see each cmd/ call site's own comment for why
// cmd/desktop does not attempt a pre-window native dialog here).
func Run(cfg Config) (*State, error) {
	dbPath, err := resolveDBPath(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	strategyPath := resolvePath(cfg.StrategyPath, EnvStrategyPath, filepath.Join(repoRoot, config.DefaultStrategyPath))
	riskPath := resolvePath(cfg.RiskPath, EnvRiskPath, filepath.Join(repoRoot, config.DefaultRiskPath))

	db, err := repository.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: open db %q: %w", dbPath, err)
	}

	strategy, err := config.LoadStrategy(strategyPath)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("bootstrap: load strategy config %q: %w", strategyPath, err)
	}

	riskCfg, err := config.LoadRisk(riskPath)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("bootstrap: load risk config %q: %w", riskPath, err)
	}

	return &State{
		DB:       db,
		Strategy: strategy,
		Risk:     riskCfg,
		Paths: ResolvedPaths{
			DBPath:       dbPath,
			StrategyPath: strategyPath,
			RiskPath:     riskPath,
		},
	}, nil
}

// Close releases the underlying DB handle. Safe to call on a nil State's
// zero-valued DB field only if State itself is non-nil; callers that hold
// a *State returned from a successful Run always have a non-nil DB.
func (s *State) Close() error {
	if s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

func resolveDBPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if v, ok := os.LookupEnv(EnvDBPath); ok && v != "" {
		return v, nil
	}
	return DefaultDBPath()
}

func resolvePath(explicit, envVar, fallback string) string {
	if explicit != "" {
		return explicit
	}
	if v, ok := os.LookupEnv(envVar); ok && v != "" {
		return v
	}
	return fallback
}
