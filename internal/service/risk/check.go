package risk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/sizing"
)

// Check implements internal/service/policy.RiskChecker: Risk Engine's
// final say over every Policy Engine trade candidate (FR-RISK-1).
//
// Rejection reasons are: ReasonKillSwitchActive/ReasonSystemPaused (Kill
// Switch/manual-pause active, FR-RISK-2/FR-RISK-4), ReasonCooldownAfterLoss
// (risk.yaml cooldown_after_loss_minutes has not elapsed since the most
// recent loss - a lightweight, time-based gate that is not itself logged
// to kill_switch_events, unlike consecutive_losses reaching
// max_consecutive_losses below), then each FR-RISK-1 limit in the table's
// order. max_trade_loss_pct is enforced through position sizing
// (sizing.Quantity): the candidate is rejected when not even one lot
// keeps a stop-out loss within it.
//
// Check fails closed: any error reading the state a limit needs
// (portfolio/snapshot lookup) or a missing latest price/spread rejects
// the candidate as ReasonRiskEngineError (issue #161) rather than
// skipping that limit.
//
// Breaching max_daily_loss_pct or max_consecutive_losses also raises a
// Kill Switch (FR-RISK-2), latching the rejection in place for every
// subsequent candidate (via the Kill Switch state check above) rather
// than only this one; RunPeriodicChecks evaluates the same two limits
// (losslimit.go) so they fire without waiting for a candidate. Both limits are measured from the manual-resume
// baseline (baseline.go), so a Resume is not immediately undone by the
// history that caused the Kill Switch.
func (e *Engine) Check(ctx context.Context, instrumentID int64, direction string) (bool, string) {
	state, events, err := e.State(ctx)
	if err != nil {
		return e.failClosed("read system state", err)
	}
	switch state {
	case domain.SystemStateKilled:
		return false, fmt.Sprintf("%s: %s", ReasonKillSwitchActive, activeReasons(events))
	case domain.SystemStatePaused:
		return false, ReasonSystemPaused
	}

	now := e.now()
	streakSince, err := e.baselineAt(ctx, settingKeyLossStreakBaselineAt)
	if err != nil {
		return e.failClosed("read loss streak baseline", err)
	}
	lastLoss, err := e.portfolio.LastLossAt(ctx, streakSince)
	if err != nil {
		return e.failClosed("read last loss time", err)
	}
	if !lastLoss.IsZero() {
		cooldown := time.Duration(e.limits.CooldownAfterLossMinutes) * time.Minute
		if resumeAt := lastLoss.Add(cooldown); now.Before(resumeAt) {
			return false, fmt.Sprintf("%s: retry_after=%s", ReasonCooldownAfterLoss, resumeAt.Format(time.RFC3339))
		}
	}

	count, err := e.portfolio.OpenPositionCount(ctx)
	if err != nil {
		return e.failClosed("read open position count", err)
	}
	if count >= e.limits.MaxOpenPositions {
		return false, fmt.Sprintf("%s: count=%d max=%d", ReasonMaxOpenPositions, count, e.limits.MaxOpenPositions)
	}
	totalPct, err := e.portfolio.TotalExposurePct(ctx)
	if err != nil {
		return e.failClosed("read total exposure", err)
	}
	if totalPct >= e.limits.MaxTotalExposurePct {
		return false, fmt.Sprintf("%s: exposure_pct=%.4f max=%.4f", ReasonMaxTotalExposurePct, totalPct, e.limits.MaxTotalExposurePct)
	}
	symbolPct, err := e.portfolio.SymbolExposurePct(ctx, instrumentID)
	if err != nil {
		return e.failClosed("read symbol exposure", err)
	}
	if symbolPct >= e.limits.MaxPositionPerSymbolPct {
		return false, fmt.Sprintf("%s: exposure_pct=%.4f max=%.4f", ReasonMaxPositionPerSymbolPct, symbolPct, e.limits.MaxPositionPerSymbolPct)
	}

	dailyLossPct, breached, err := e.dailyLossBreached(ctx)
	if err != nil {
		return e.failClosed("read daily loss", err)
	}
	if breached {
		if err := e.triggerDailyLoss(ctx, dailyLossPct); err != nil {
			slog.Error("risk: daily_loss_limit kill switch failed", "error", err)
		}
		return false, fmt.Sprintf("%s: daily_loss_pct=%.4f max=%.4f", ReasonMaxDailyLossPct, dailyLossPct, e.limits.MaxDailyLossPct)
	}
	losses, breached, err := e.consecutiveLossesBreached(ctx, streakSince)
	if err != nil {
		return e.failClosed("read consecutive losses", err)
	}
	if breached {
		if err := e.triggerConsecutiveLosses(ctx, losses); err != nil {
			slog.Error("risk: consecutive_losses kill switch failed", "error", err)
		}
		return false, fmt.Sprintf("%s: consecutive_losses=%d max=%d", ReasonMaxConsecutiveLosses, losses, e.limits.MaxConsecutiveLosses)
	}

	if e.snapshots == nil {
		return true, ""
	}
	snapshot, err := e.latestSnapshot(ctx, instrumentID)
	if err != nil {
		return e.failClosed("read latest snapshot", err)
	}
	if snapshot.SpreadBps == nil {
		return false, fmt.Sprintf("%s: spread_bps missing (data_missing)", ReasonRiskEngineError)
	}
	if *snapshot.SpreadBps > e.limits.MaxSpreadBps {
		return false, fmt.Sprintf("%s: spread_bps=%.2f max=%.2f", ReasonMaxSpreadBps, *snapshot.SpreadBps, e.limits.MaxSpreadBps)
	}
	if qty, reason := sizing.Quantity(e.limits, e.stopLossPct, snapshot.Price, totalPct); qty == 0 {
		return false, fmt.Sprintf("%s: price=%.2f stop_loss_pct=%.2f max_trade_loss_pct=%.4f", reason, snapshot.Price, e.stopLossPct, e.limits.MaxTradeLossPct)
	}

	return true, ""
}

// failClosed logs err and returns Check's ReasonRiskEngineError rejection.
func (e *Engine) failClosed(what string, err error) (bool, string) {
	slog.Error("risk: check failed closed", "step", what, "error", err)
	return false, fmt.Sprintf("%s: %s: %v", ReasonRiskEngineError, what, err)
}

var errNoSnapshot = errors.New("no market snapshot for instrument")

func (e *Engine) latestSnapshot(ctx context.Context, instrumentID int64) (domain.Snapshot, error) {
	snapshots, err := e.snapshots.ListByInstrument(ctx, instrumentID, 1)
	if err != nil {
		return domain.Snapshot{}, err
	}
	if len(snapshots) == 0 {
		return domain.Snapshot{}, errNoSnapshot
	}
	return snapshots[0], nil
}
