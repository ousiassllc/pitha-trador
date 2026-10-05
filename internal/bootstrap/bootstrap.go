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

	configdefaults "github.com/ousiassllc/pitha-trador/config"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/startup"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
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

// embeddedConfigSource is the ResolvedPaths.StrategyPath/RiskPath value
// Run reports when it fell back to configdefaults' compiled-in YAML
// rather than reading any file from disk - i.e. none of an explicit
// bootstrap.Config field, the PITHA_STRATEGY_PATH/PITHA_RISK_PATH
// environment variable, nor a config/*.yaml next to the running
// executable (loadConfigOrEmbedded's steps 1-3) applied
// (docs/architecture/overview.md §9, issue #59).
const embeddedConfigSource = "(embedded default)"

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
// empty falls back, in order, to: the corresponding PITHA_*_PATH
// environment variable (Env* below), a config/*.yaml next to the running
// executable (os.Executable), then finally DefaultDBPath/the
// configdefaults package's compiled-in strategy.yaml/risk.yaml
// (loadConfigOrEmbedded's four steps, issue #59). DBPath has no
// executable-relative or embedded step - see DefaultDBPath.
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
	LogDir       string
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
// overrides. DBPath falls back through EnvDBPath to DefaultDBPath
// (resolveDBPath); StrategyPath/RiskPath fall back through
// EnvStrategyPath/EnvRiskPath, then a config/*.yaml next to the running
// executable, then a compiled-in default (loadConfigOrEmbedded) - see
// Config's doc comment for the full four-step precedence issue #59
// introduced so a `wails build`/`go build ./cmd/server` .exe distributed
// with no accompanying config/ directory still starts.
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

	db, err := sqlitedb.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: open db %q: %w", dbPath, err)
	}

	strategy, strategyPath, err := loadConfigOrEmbedded(
		cfg.StrategyPath, EnvStrategyPath, config.DefaultStrategyPath,
		configdefaults.DefaultStrategyYAML, config.LoadStrategy, config.LoadStrategyBytes,
	)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	riskCfg, riskPath, err := loadConfigOrEmbedded(
		cfg.RiskPath, EnvRiskPath, config.DefaultRiskPath,
		configdefaults.DefaultRiskYAML, config.LoadRisk, config.LoadRiskBytes,
	)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return &State{
		DB:       db,
		Strategy: strategy,
		Risk:     riskCfg,
		Paths: ResolvedPaths{
			DBPath:       dbPath,
			StrategyPath: strategyPath,
			RiskPath:     riskPath,
			LogDir:       startup.LogDir(dbPath),
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

// loadConfigOrEmbedded resolves and loads one of strategy.yaml/risk.yaml,
// returning the parsed value alongside the source path Run reports on
// State.Paths (embeddedConfigSource when step 4 - the embedded fallback -
// was used). explicit/envVar/defaultRelPath/embedded/loadFile/loadBytes
// mirror resolveConfigPath's parameters plus the two internal/config
// loader functions (config.LoadStrategy+config.LoadStrategyBytes, or
// config.LoadRisk+config.LoadRiskBytes) for the config type T in
// question.
func loadConfigOrEmbedded[T any](
	explicit, envVar, defaultRelPath string,
	embedded []byte,
	loadFile func(string) (*T, error),
	loadBytes func([]byte) (*T, error),
) (*T, string, error) {
	if path, ok := resolveConfigPath(explicit, envVar, defaultRelPath); ok {
		v, err := loadFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("bootstrap: load config %q: %w", path, err)
		}
		return v, path, nil
	}

	v, err := loadBytes(embedded)
	if err != nil {
		return nil, "", fmt.Errorf("bootstrap: load embedded default config: %w", err)
	}
	return v, embeddedConfigSource, nil
}

// resolveConfigPath resolves a strategy.yaml/risk.yaml path following
// issue #59's precedence: explicit (Config's own StrategyPath/RiskPath
// field) first, then envVar (EnvStrategyPath/EnvRiskPath), then
// defaultRelPath (config.DefaultStrategyPath/config.DefaultRiskPath, e.g.
// "config/strategy.yaml") resolved next to the running executable
// (os.Executable) - but only if that file actually exists there, so a
// bare `go build`/`wails build` .exe with no accompanying config/
// directory doesn't get a nonexistent-file error. ok is false when none
// of the three apply; loadConfigOrEmbedded then falls back to its
// compiled-in embedded default (step 4) instead.
func resolveConfigPath(explicit, envVar, defaultRelPath string) (path string, ok bool) {
	if explicit != "" {
		return explicit, true
	}
	if v, ok := os.LookupEnv(envVar); ok && v != "" {
		return v, true
	}
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	candidate := filepath.Join(filepath.Dir(exe), defaultRelPath)
	if _, err := os.Stat(candidate); err != nil {
		return "", false
	}
	return candidate, true
}

// ResolveLogDir returns the absolute log directory (startup.LogDir) for the
// DB path resolved like Run does, independent of the working directory.
func ResolveLogDir() (string, error) {
	dbPath, err := resolveDBPath("")
	if err != nil {
		return "", err
	}
	return startup.LogDir(dbPath), nil
}
