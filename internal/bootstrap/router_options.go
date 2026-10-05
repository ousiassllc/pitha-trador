package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/scanner"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/symbol"
)

// LoadSecrets reads the required JEV_API_KEY/KABU_API_PASSWORD (plus the
// optional JEV_BASE_URL/JEV_MODEL/SLACK_WEBHOOK_URL and LUNA_*/NEWS_FEED_*/
// SOL_*/OPUS_* AI/News API credentials) from the secrets table (issue #57 -
// `.env`/environment variables are no longer a supported input for these at
// all). Unlike the old env-var-based loader, a missing value is never
// fatal: the app starts regardless, missing only drives a startup warning
// log and the Settings screen's (`/settings`) header banner - BuildServices'
// Jev/kabuステーションAPI client wiring simply receives empty strings for
// anything unset (both marketdata.NewClient/jev.NewClient tolerate that)
// until an operator fills them in and restarts (no hot-reload).
//
// The returned repository is the same store RouterOptions wires into the
// Settings screen and Setup Guard. err is non-nil only for an actual
// repository/DB failure.
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
// Entrypoint-specific options (WithAllowedHosts, WithWebSocketBase and
// cmd/desktop's WithUpdateController - services.Updater is nil on
// cmd/server, which a typed-nil interface would not reveal) are appended by
// the caller.
func RouterOptions(services *Services, state *State, secretsRepo *system.SecretsRepository) []router.Option {
	scan := state.Strategy.Scan
	return []router.Option{
		router.WithCandidateSource(services.Screener),
		router.WithSystemEngine(services.Risk),
		router.WithHeartbeatRecorder(services.Risk),
		router.WithSymbolProvider(services.Execution),
		router.WithSymbolRiskParams(symbol.NewSymbolRiskParams(services.Risk.Limits(), services.Execution.Config(), services.Risk.AllowedPositionPct)),
		router.WithInsightProvider(services.Insight),
		router.WithCalibrationSource(services.Calibration),
		router.WithPolicyProposalSource(services.Proposals),
		router.WithBacktestRunner(services.Backtest),
		router.WithActivitySource(services.Activity),
		router.WithSecretsStore(secretsRepo),
		router.WithMarketDataStatus(services.MarketData),
		router.WithErrorLogExporter(services.ErrorLogs),
		router.WithCandidateRefreshInterval(scanner.CandidateRefreshInterval{
			Min: time.Duration(scan.CandidateRefreshIntervalSecondsMin) * time.Second,
			Max: time.Duration(scan.CandidateRefreshIntervalSecondsMax) * time.Second,
		}),
	}
}
