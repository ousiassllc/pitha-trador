package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/evalprovider"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/exitalignjob"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/paperrisk"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/proposalview"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/startup"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/universe"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/dataexport"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/scanner"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/symbol"
)

// LoadSecrets reads the required JEV_API_KEY/KABU_API_PASSWORD (plus the optional
// JEV_BASE_URL/JEV_MODEL/SLACK_WEBHOOK_URL and LUNA_*/NEWS_FEED_*/SOL_*/OPUS_* AI/News API
// credentials) from the secrets table (issue #57 - `.env`/environment variables are no longer a
// supported input for these at all). Unlike the old env-var-based loader, a missing value is never
// fatal: the app starts regardless, missing only drives a startup warning log and the Settings
// screen's (`/settings`) header banner - BuildServices' Jev/kabuステーションAPI client wiring simply
// receives empty strings for anything unset (both marketdata.NewClient/jev.NewClient tolerate that)
// until an operator fills them in and restarts (no hot-reload).
//
// The returned repository is the same store RouterOptions wires into the Settings screen and Setup
// Guard. err is non-nil only for an actual repository/DB failure.
func LoadSecrets(ctx context.Context, state *State) (*system.SecretsRepository, config.Secrets, error) {
	secretsRepo := system.NewSecretsRepository(state.DB)
	secrets, missing, err := config.LoadSecretsFromDB(ctx, secretsRepo)
	if err != nil {
		return nil, config.Secrets{}, err
	}
	if len(missing) > 0 {
		slog.Warn("bootstrap: secrets not yet configured; configure them at /settings and restart", "missing", missing)
	}
	return secretsRepo, secrets, nil
}

// RouterOptions returns the router.Options common to cmd/desktop and
// cmd/server: every data source/handler dependency BuildServices produced.
// Entrypoint-specific options (WithAllowedHosts, WithWebSocketBase,
// mcpwire.RouterOptions (the Settings MCP card, issue #795) and
// cmd/desktop's WithUpdateController - services.Updater is nil on
// cmd/server, which a typed-nil interface would not reveal) are appended by
// the caller.
func RouterOptions(services *Services, state *State, secretsRepo *system.SecretsRepository) []router.Option {
	scan := state.Strategy.Scan
	// The MCP's write switch and audit log live in the application's database;
	// the Settings screen's own changes to the keys the MCP may write are
	// recorded there too (issue #806).
	mcpGate := mcpset.NewGate(state.DB)
	opts := []router.Option{
		router.WithCandidateSource(services.Screener),
		router.WithSystemEngine(services.Risk),
		router.WithHeartbeatRecorder(services.Risk),
		router.WithSymbolProvider(services.Execution),
		router.WithSymbolRiskParams(symbol.NewSymbolRiskParams(services.Risk.Limits(), services.Execution.Config(), services.Risk.AllowedPositionPct)),
		router.WithInsightProvider(evalprovider.New(services.Insight, state.DB, paperrisk.CapitalNow(services.Risk))),
		router.WithCalibrationSource(exitalignjob.NewCalibrationSource(services.Calibration, state.DB, services.Settings, services.Execution.Config())),
		router.WithPolicyProposalSource(services.Proposals),
		router.WithBacktestRunner(services.Backtest),
		router.WithActivitySource(services.Activity),
		router.WithOrderProposalSource(proposalview.Reader{DB: state.DB}),
		router.WithSecretsStore(secretsRepo),
		router.WithOperationalSettings(mcpset.NewAudited(opsettings.New(services.Settings, *state.Strategy, startup.DefaultLogDir(state.Paths.DBPath)).WithRiskReference(services.Risk.Limits()).WithTradingMode(metalabelsettings.RuntimeMode), mcpGate)),
		router.WithMCPWriteGate(mcpGate),
		router.WithMarketDataStatus(services.Broker),
		router.WithDailyHaltSource(services.Risk),
		router.WithHealthAlertSource(services.HealthAlerts),
		router.WithBrokerSession(services.Broker),
		router.WithErrorLogExporter(services.ErrorLogs),
		router.WithDataExporter(dataexport.NewLocal(state.DB, services.ErrorLogs)),
		router.WithUniverseImporter(universe.NewImporter(services.Instruments, nil)),
		router.WithCandidateRefreshInterval(scanner.CandidateRefreshInterval{
			Min: time.Duration(scan.CandidateRefreshIntervalSecondsMin) * time.Second,
			Max: time.Duration(scan.CandidateRefreshIntervalSecondsMax) * time.Second,
		}),
	}
	return append(opts, services.watchlistRouterOptions()...)
}
