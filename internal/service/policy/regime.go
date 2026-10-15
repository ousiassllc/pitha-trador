package policy

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/regimesettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
	"github.com/ousiassllc/pitha-trador/internal/service/regime"
)

// RegimeAssessor classifies the market regime and the A/B arm of a decision on
// bar, the instrument's latest market snapshot (internal/service/regime.
// Assessor implements it).
type RegimeAssessor interface {
	Assess(ctx context.Context, bar domain.Snapshot) marketregime.Assessment
}

// WithRegimeAssessor makes Handler assess every Jev trader decision's market
// regime before the call: the assessment is stored with the decision
// (jev_decisions.market_regime, ab_arm, ...) and, on the regime arm in a
// mean-reverting regime, switches the entry rule to the VWAP reversion
// (issue #755). Without it nothing is assessed and every decision trades the
// plain momentum strategy.
func WithRegimeAssessor(a RegimeAssessor) HandlerOption {
	return func(h *Handler) { h.regime = a }
}

// WithRegimeStore is WithRegimeAssessor with the production assessor
// (regime.Assessor): it reads the tracked market indices from instruments and
// snapshots, the Settings from store at every decision, and counts an index bar
// older than maxStale as no data.
func WithRegimeStore(instruments regime.Instruments, snapshots regime.Snapshots, store regimesettings.Store, maxStale time.Duration) HandlerOption {
	return WithRegimeAssessor(regime.NewAssessor(instruments, snapshots, regime.FromStore(store), maxStale))
}

// Reasons of a NONE the VWAP reversion rule produced (FR-POLICY-3's
// reject_reason prefixes).
const (
	// ReasonVWAPZUnavailable: a mean-reverting regime on the regime arm, but
	// the stock's VWAP deviation could not be measured (too few bars since the
	// open, no VWAP, or a stock that never left VWAP).
	ReasonVWAPZUnavailable = "vwap_z_unavailable"
	// ReasonVWAPNotExtreme: the deviation from VWAP is inside the Settings
	// z-score threshold, so there is nothing to revert.
	ReasonVWAPNotExtreme = "vwap_not_extreme"
)

// chooseSide picks the side to evaluate and the thresholds that govern it.
// Normally it is Jev's direction (momentum). On the regime arm in a
// mean-reverting regime (issue #755) it is the VWAP reversion side instead,
// and reversion is true: the price is stretched away from VWAP, so the trade
// fades the stretch - a SHORT above VWAP, a LONG below - whatever Jev's
// momentum direction is. reason is the NONE reason when there is no side to
// evaluate (Jev has no direction, the stock cannot be sold short, or the
// deviation is not extreme).
func chooseSide(in Input, th Thresholds) (side string, thresholds config.PolicyDirectionThresholds, reversion bool, reason *string) {
	d := in.Decision
	if assessment := d.MarketRegime; assessment.UsesVWAPReversion() {
		side, reason = reversionSide(assessment)
		reversion = true
		if reason != nil {
			return "", thresholds, reversion, reason
		}
	} else {
		switch *d.Direction {
		case domain.JevDirectionLong, domain.JevDirectionShort:
			side = *d.Direction
		default:
			return "", thresholds, false, reasonf(ReasonJevNone)
		}
	}
	if side == domain.JevDirectionShort {
		if in.Lendable != nil && !*in.Lendable {
			return "", thresholds, reversion, reasonf(ReasonNotLendable)
		}
		return side, th.Policy.Short, reversion, nil
	}
	return side, th.Policy.Long, reversion, nil
}

// reversionSide is the side of the VWAP reversion entry for a: SHORT when the
// price is at least the z-score threshold above VWAP, LONG when it is at least
// that far below. reason is set (and side empty) when the deviation is
// unavailable or inside the threshold.
func reversionSide(a marketregime.Assessment) (side string, reason *string) {
	if a.VWAPZ == nil {
		return "", reasonf(ReasonVWAPZUnavailable)
	}
	threshold := a.VWAPZThreshold
	if threshold <= 0 {
		threshold = marketregime.DefaultVWAPZThreshold
	}
	z := *a.VWAPZ
	switch {
	case z >= threshold:
		return domain.JevDirectionShort, nil
	case z <= -threshold:
		return domain.JevDirectionLong, nil
	}
	return "", reasonf("%s: vwap_z=%.2f within ±%.2f", ReasonVWAPNotExtreme, z, threshold)
}
