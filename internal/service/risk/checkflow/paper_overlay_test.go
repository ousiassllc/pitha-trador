package checkflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// overlayEngine is an Engine over a fresh DB with one 2,000-yen snapshot and
// the given overlay (nil: none, i.e. a Live-style Engine), plus the store the
// Settings screen writes the Paper rows to.
func overlayEngine(t *testing.T, limits config.RiskLimits, portfolio risk.PortfolioProvider, overlay func(*system.RuntimeSettingsRepository) func(context.Context, config.RiskLimits) (config.RiskLimits, error)) (*risk.Engine, *system.RuntimeSettingsRepository) {
	t.Helper()
	db := newTestDB(t)
	snapshots := market.NewSnapshotRepository(db)
	seedSnapshot(t, db, snapshots, 2000, 5)
	settings := system.NewRuntimeSettingsRepository(db)
	cfg := risk.Config{
		Limits:     limits,
		KillSwitch: system.NewKillSwitchRepository(db),
		Settings:   settings,
		Snapshots:  snapshots,
		Portfolio:  portfolio,
	}
	if overlay != nil {
		cfg.LimitsOverlay = overlay(settings)
	}
	return risk.NewEngine(cfg), settings
}

func paperOverlay(s *system.RuntimeSettingsRepository) func(context.Context, config.RiskLimits) (config.RiskLimits, error) {
	return papersettings.Overlay(s)
}

func storePaper(t *testing.T, s *system.RuntimeSettingsRepository, capital, symbolPct, totalPct, tradeLossPct string) {
	t.Helper()
	for key, value := range map[string]string{
		papersettings.KeyInitialCapitalJPY:       capital,
		papersettings.KeyMaxPositionPerSymbolPct: symbolPct,
		papersettings.KeyMaxTotalExposurePct:     totalPct,
		papersettings.KeyMaxTradeLossPct:         tradeLossPct,
	} {
		if err := s.Set(context.Background(), key, value, time.Now()); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}
}

// issue #804: a Paper value saved on the Settings screen applies to the very
// next evaluation of the same Engine - no restart.
func TestPaperOverlay_SavedValuesApplyToTheNextEvaluation(t *testing.T) {
	ctx := context.Background()
	e, settings := overlayEngine(t, testLimits(), fakePortfolio{symbolExposurePct: 50, totalExposurePct: 50}, paperOverlay)

	// risk.yaml: 2% per symbol, 20% in total -> 50% held is over both.
	if passed, reason := e.Check(ctx, 1, domain.JevDirectionLong); passed || !strings.HasPrefix(reason, risk.ReasonMaxTotalExposurePct) {
		t.Fatalf("Check before saving = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonMaxTotalExposurePct)
	}
	if got := e.PositionSize(ctx, 2000); got != 0 {
		t.Fatalf("PositionSize before saving = %d, want 0 (exposure over the yaml cap)", got)
	}

	storePaper(t, settings, "500000", "100", "100", "1.0")

	if passed, reason := e.Check(ctx, 1, domain.JevDirectionLong); !passed {
		t.Fatalf("Check after saving 500000/100/100/1.0 = (false, %q), want passed", reason)
	}
	limits, err := e.EffectiveLimits(ctx)
	if err != nil || limits.InitialCapital != 500_000 || limits.MaxPositionPerSymbolPct != 100 || limits.MaxTotalExposurePct != 100 {
		t.Fatalf("EffectiveLimits = %+v, %v", limits, err)
	}
	if limits.MaxTradeLossPct != 1.0 || limits.MaxOpenPositions != 5 {
		t.Fatalf("the overlay changed a limit other than the four Paper values: %+v", limits)
	}
}

// issue #804: with the Paper initial values a 2,000-yen stock is sized from
// the 500,000-yen capital (the PositionSize denominator), not the 30M yaml one.
func TestPaperOverlay_SizingUsesTheSavedCapital(t *testing.T) {
	ctx := context.Background()
	e, settings := overlayEngine(t, testLimits(), risk.ZeroPortfolioProvider{}, paperOverlay)
	storePaper(t, settings, "500000", "100", "100", "1.0")

	// 500,000 x 1.0% = 5,000 yen of loss / (2,000 x 0.6% = 12 yen) = 416 shares by
	// trade loss, 500,000 x 100% / 2,000 = 250 shares by the capital -> two lots.
	if got := e.PositionSize(ctx, 2000); got != 200 {
		t.Fatalf("PositionSize = %d, want 200", got)
	}
	// 200 shares x 2,000 yen is 80% of 500,000 yen.
	if got := e.AllowedPositionPct(ctx, 2000); got != 80 {
		t.Fatalf("AllowedPositionPct = %v, want 40", got)
	}
}

// issue #804: an unreadable or damaged row never loosens a limit: it is
// skipped (the yaml value applies); an unreadable store fails the evaluation
// closed.
func TestPaperOverlay_DamagedRowFallsBackToYamlAndStoreErrorFailsClosed(t *testing.T) {
	ctx := context.Background()
	e, settings := overlayEngine(t, testLimits(), risk.ZeroPortfolioProvider{}, paperOverlay)
	for key, bad := range map[string]string{
		papersettings.KeyInitialCapitalJPY:       "0",
		papersettings.KeyMaxPositionPerSymbolPct: "150",
		papersettings.KeyMaxTotalExposurePct:     `"abc"`,
		papersettings.KeyMaxTradeLossPct:         "5",
	} {
		if err := settings.Set(ctx, key, bad, time.Now()); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}
	limits, err := e.EffectiveLimits(ctx)
	if err != nil || limits != testLimits() {
		t.Fatalf("EffectiveLimits with damaged rows = %+v, %v; want the yaml limits", limits, err)
	}

	failing := func(*system.RuntimeSettingsRepository) func(context.Context, config.RiskLimits) (config.RiskLimits, error) {
		return func(context.Context, config.RiskLimits) (config.RiskLimits, error) {
			return config.RiskLimits{}, errors.New("settings store down")
		}
	}
	e, _ = overlayEngine(t, testLimits(), risk.ZeroPortfolioProvider{}, failing)
	if passed, reason := e.Check(ctx, 1, domain.JevDirectionLong); passed || !strings.HasPrefix(reason, risk.ReasonRiskEngineError) {
		t.Errorf("Check with an unreadable overlay = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonRiskEngineError)
	}
	if got := e.PositionSize(ctx, 2000); got != 0 {
		t.Errorf("PositionSize with an unreadable overlay = %d, want 0", got)
	}
}

// issue #804: Live keeps using config/risk.yaml only. An Engine built without
// the overlay (how a Live Engine is built) enforces its Limits whatever the
// Paper runtime_settings rows hold.
func TestLiveEngine_IgnoresPaperRuntimeSettings(t *testing.T) {
	ctx := context.Background()
	live := testLimits()
	live.InitialCapital = 10_000_000
	live.MaxPositionPerSymbolPct = 1
	live.MaxTotalExposurePct = 10
	e, settings := overlayEngine(t, live, fakePortfolio{symbolExposurePct: 5, totalExposurePct: 5}, nil)
	storePaper(t, settings, "500000", "100", "100", "1.0")

	// 5% held is over Live's 1% per-symbol cap even though the Paper row says 100%.
	if passed, reason := e.Check(ctx, 1, domain.JevDirectionLong); passed || !strings.HasPrefix(reason, risk.ReasonMaxPositionPerSymbolPct) {
		t.Fatalf("Check = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonMaxPositionPerSymbolPct)
	}
	limits, err := e.EffectiveLimits(ctx)
	if err != nil || limits != live {
		t.Fatalf("EffectiveLimits = %+v, %v; want the Live limits unchanged", limits, err)
	}
	// Sizing: 10M x 0.25% / (2,000 x 0.6%) = 2,083 shares by trade loss, 10M x 1% / 2,000 = 50 shares by the per-symbol cap -> no lot.
	if got := e.PositionSize(ctx, 2000); got != 0 {
		t.Fatalf("PositionSize = %d, want 0 under Live's yaml limits", got)
	}
}

// The same pin for the fourth key: Live's max_trade_loss_pct (tiny here, so no
// lot fits) is not loosened by the Paper row, and the Paper Engine's own 1.0%
// applies to it. Paper-side: the saved value changes the very next sizing.
func TestLiveEngine_IgnoresPaperTradeLossSetting(t *testing.T) {
	ctx := context.Background()
	live := testLimits()
	live.InitialCapital = 10_000_000
	live.MaxPositionPerSymbolPct = 100
	live.MaxTotalExposurePct = 100
	live.MaxTradeLossPct = 0.0001
	e, settings := overlayEngine(t, live, risk.ZeroPortfolioProvider{}, nil)
	storePaper(t, settings, "500000", "100", "100", "3.0")

	if got := e.PositionSize(ctx, 2000); got != 0 {
		t.Fatalf("Live PositionSize = %d, want 0: its max_trade_loss_pct is the yaml 0.0001, not the Paper row", got)
	}
	if passed, reason := e.Check(ctx, 1, domain.JevDirectionLong); passed || !strings.HasPrefix(reason, risk.ReasonMaxTradeLossPct) {
		t.Fatalf("Live Check = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonMaxTradeLossPct)
	}
	if limits, err := e.EffectiveLimits(ctx); err != nil || limits != live {
		t.Fatalf("Live EffectiveLimits = %+v, %v; want unchanged", limits, err)
	}

	// The Paper Engine with the same yaml reads the row: 3.0% of 500,000 = 15,000
	// yen of loss allows 1,250 shares, the 500,000 yen capital 250 -> two lots.
	paper, paperSettings := overlayEngine(t, live, risk.ZeroPortfolioProvider{}, paperOverlay)
	storePaper(t, paperSettings, "500000", "100", "100", "3.0")
	if got := paper.PositionSize(ctx, 2000); got != 200 {
		t.Fatalf("Paper PositionSize = %d, want 200 under the saved 3.0%%", got)
	}
}

// issue #804 acceptance: with 500000/100/100/1.0 saved, a 5,000-yen stock's
// 100-share lot (500,000 yen) is not rejected by sizing.
func TestPaperOverlay_FiveThousandYenLotIsNotRejectedBySizing(t *testing.T) {
	ctx := context.Background()
	e, settings := overlayEngine(t, testLimits(), risk.ZeroPortfolioProvider{}, paperOverlay)
	storePaper(t, settings, "500000", "100", "100", "1.0")
	if got := e.PositionSize(ctx, 5000); got != 100 {
		t.Fatalf("PositionSize(5000) = %d, want 100", got)
	}
	if got := e.AllowedPositionPct(ctx, 5000); got != 100 {
		t.Fatalf("AllowedPositionPct(5000) = %v, want 100", got)
	}
}
