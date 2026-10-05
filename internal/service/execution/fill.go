package execution

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// fill is one order's execution under Config.Fill: the price, the
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

// fillFor prices an order of orderType/side/qty at now against the last
// price ref and its book (fillmodel: tick grid, spread, slippage, fee,
// 寄り/引けの板寄せ). ok=false means a limit order that does not fill yet; a
// closed market (昼休み・立会時間外) is ErrOutsideTradingSession.
func (e *Engine) fillFor(orderType, side string, qty int64, limit *float64, ref float64, book fillmodel.Book, now time.Time) (f fill, ok bool, err error) {
	phase := e.phaseAt(now)
	if phase == marketcalendar.PhaseClosed {
		return fill{}, false, ErrOutsideTradingSession
	}
	if orderType == domain.OrderTypeLimit {
		f.price, ok = e.cfg.Fill.Limit(side, *limit, ref, book, phase)
	} else if f.price, err = e.cfg.Fill.Market(side, ref, book, phase); err == nil {
		ok = true
	}
	f.fee = e.cfg.Fill.Fee(f.price, qty)
	f.slippageBps = fillmodel.SlippageBps(side, ref, f.price)
	return f, ok, err
}
