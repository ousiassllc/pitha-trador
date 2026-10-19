package backtestsource

import (
	"database/sql"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

// NewForDB builds the Source of db exactly as bootstrap.NewServices does (the
// same thresholds, Paper risk limits, exit/fill config with the TSE calendar,
// and runtime Settings), but over the repositories alone, so it works on a
// database opened read-only. cmd/backtestverify runs
// Source.VerifyReproducibility with it (FR-BT-4).
func NewForDB(db *sql.DB, strategy *config.StrategyConfig, risk *config.RiskConfig) *Source {
	settings := system.NewRuntimeSettingsRepository(db)
	runtimePolicy := selfimprove.NewRuntimePolicy(settings, judgement.NewProposalRepository(db), strategy.Policy)
	executionConfig := execution.ConfigFromRiskLimits(risk.Paper).WithCostSettings(settings)
	executionConfig.Calendar = marketcalendar.TSE
	return New(
		market.NewInstrumentRepository(db), market.NewSnapshotRepository(db), judgement.NewDecisionRepository(db),
		policy.ThresholdsFromStrategy(*strategy), runtimePolicy, executionConfig,
		NewReplay(db, settings, risk.Paper, trading.NewPositionRepository(db)),
	)
}
