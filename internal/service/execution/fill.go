package execution

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// fill is one order's execution under the cost assumption: the price, the
// commission (JPY) and the slippage versus the reference price (bps).
type fill struct {
	price, fee, slippageBps float64
}

// phaseAt is how an order at now executes; without a Calendar every
// moment is ザラ場.
func (e *Engine) phaseAt(now time.Time) marketcalendar.Phase {
	if e.cfg.Calendar == nil {
		return marketcalendar.PhaseContinuous
	}
	return e.cfg.Calendar.PhaseAt(now)
}

// costsFor is the cost assumption of a position of direction in force now:
// the Settings read through Config.Costs, or Config.Fill without one or when
// the read fails (an exit must never be blocked by an unreadable setting).
func (e *Engine) costsFor(ctx context.Context, direction string) fillmodel.Model {
	if e.cfg.Costs != nil {
		costs, err := e.cfg.Costs.Costs(ctx)
		if err == nil {
			return costs.For(direction)
		}
		slog.WarnContext(ctx, "execution: cost settings unreadable; filling with the configured costs", "error", err)
	}
	return e.cfg.Fill.For(direction)
}

// fillFor prices an order of orderType/side/qty at now against the last
// price ref and its book (fillmodel: tick grid, spread, slippage, fee,
// 寄り/引けの板寄せ). ok=false means a limit order that does not fill yet; a
// closed market (昼休み・立会時間外) is ErrOutsideTradingSession. direction is
// the position's (LONG/SHORT), which selects the cost assumption: an entry
// and its exit are priced with the same direction's slippage.
func (e *Engine) fillFor(ctx context.Context, direction, orderType, side string, qty int64, limit *float64, ref float64, book fillmodel.Book, now time.Time) (f fill, ok bool, err error) {
	phase := e.phaseAt(now)
	costs := e.costsFor(ctx, direction)
	if phase == marketcalendar.PhaseClosed {
		return fill{}, false, ErrOutsideTradingSession
	}
	if orderType == domain.OrderTypeLimit {
		f.price, ok = costs.Limit(side, *limit, ref, book, phase)
	} else if f.price, err = costs.Market(side, ref, book, phase); err == nil {
		ok = true
	}
	f.fee = costs.Fee(f.price, qty)
	f.slippageBps = fillmodel.SlippageBps(side, ref, f.price)
	return f, ok, err
}
