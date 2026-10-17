package backtest

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/simrisk"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/vwapcross"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// instState is one instrument's replay state.
type instState struct {
	cfg    RunConfig
	bars   []domain.Snapshot
	engine *policy.Engine
	x      executor

	// consumedUpTo: a Jev decision at or before it was already evaluated (or
	// arrived while a position was open) and is treated as absent (FR-BT-4:
	// one decision, at most one entry - across Walk Forward windows too).
	consumedUpTo time.Time
	open         *openTrade
	// cooldownUntil is the end of the per-symbol pause after a losing close.
	cooldownUntil time.Time
}

type event struct {
	ts   time.Time
	inst int
	idx  int
}

// sim replays the live decision flow over one or more instruments in
// chronological order, sharing one simulated Risk Engine portfolio.
type sim struct {
	insts     []*instState
	window    Period
	portfolio *simrisk.Portfolio // nil: no Risk Engine
	trades    []Trade
	openCount int
}

// replay walks every cfg's Snapshots forward through window together, bar by
// bar in time order, opening a Trade when the entry rule (cfg.Entry) and the
// Risk Engine (cfg.Risk) allow it, and closing it by the FR-EXIT-1 conditions
// (cfg.Exit) - one open position per instrument, matching functional.md
// §4.9's per-symbol "position" state. Entries happen only in [window.Start,
// window.End); a position opened there is managed on, past window.End, until
// it exits or the data ends. When entries is false (the Training/Calibration
// period: this setup has not been calibrated yet) no entry is made.
func replay(ctx context.Context, cfgs []RunConfig, window Period, entries bool) ([]Trade, error) {
	s, err := newSim(cfgs, window)
	if err != nil {
		return nil, err
	}
	for _, ev := range s.events() {
		inWindow := ev.ts.Before(window.End)
		if !inWindow && s.openCount == 0 {
			break
		}
		st := s.insts[ev.inst]
		if !inWindow && st.open == nil {
			continue
		}
		if s.portfolio != nil {
			s.portfolio.Advance(ev.ts)
			s.portfolio.Observe(st.cfg.InstrumentID, st.bars[ev.idx].Price)
		}
		if st.open != nil {
			err = s.stepOpen(ctx, st, ev.idx)
		} else {
			err = s.stepFlat(ctx, st, ev.idx, entries)
		}
		if err != nil {
			return nil, err
		}
	}
	s.closeAtDataEnd()
	return s.trades, nil
}

func newSim(cfgs []RunConfig, window Period) (*sim, error) {
	s := &sim{window: window}
	var risk *simrisk.Config
	for i, cfg := range cfgs {
		if i == 0 {
			risk = cfg.Risk
		} else if (cfg.Risk == nil) != (risk == nil) || (risk != nil && *cfg.Risk != *risk) {
			return nil, fmt.Errorf("backtest: %s: Risk differs from the other instruments' (one portfolio shares one Risk Engine)", cfg.Symbol)
		}
		st := &instState{cfg: cfg, bars: cfg.Snapshots, x: executor{cost: cfg.Cost, sessions: cfg.Sessions}}
		var checker policy.RiskChecker
		if s.portfolio == nil && risk != nil {
			s.portfolio = simrisk.New(*risk)
		}
		if s.portfolio != nil {
			checker = s.portfolio
		}
		st.engine = policy.NewEngine(cfg.Thresholds, checker, nil)
		if start := st.startIndex(window); start > 0 {
			// A decision visible at the window's first bar was recorded before
			// the window: a previous window (or the warm-up history) already
			// evaluated or skipped it.
			st.consumedUpTo = st.bars[start-1].Timestamp
		}
		s.insts = append(s.insts, st)
	}
	return s, nil
}

func (st *instState) startIndex(window Period) int {
	return sort.Search(len(st.bars), func(k int) bool { return !st.bars[k].Timestamp.Before(window.Start) })
}

// events lists every bar from window.Start on, across instruments, in time
// order (ties in the order of the configs).
func (s *sim) events() []event {
	var events []event
	for i, st := range s.insts {
		for idx := st.startIndex(s.window); idx < len(st.bars); idx++ {
			events = append(events, event{ts: st.bars[idx].Timestamp, inst: i, idx: idx})
		}
	}
	slices.SortStableFunc(events, func(a, b event) int { return a.ts.Compare(b.ts) })
	return events
}

// stepFlat evaluates a possible entry on bars[idx] of an instrument with no
// open position: the flow of the live pipeline from the recorded Jev Trader
// decision on - hard guards, entry rule, Risk Engine, fill, sizing.
func (s *sim) stepFlat(ctx context.Context, st *instState, idx int, entries bool) error {
	bar := st.bars[idx]
	cfg := st.cfg
	decision, ok, err := cfg.Decisions.Decision(ctx, cfg.InstrumentID, bar.Timestamp)
	if err != nil {
		return fmt.Errorf("backtest: decision lookup for %q at %s: %w", cfg.Symbol, bar.Timestamp, err)
	}
	// Each Jev Trader decision is evaluated at most once (FR-BT-4): live, one
	// decision yields at most one Policy evaluation/signal.
	if ok && !decision.Timestamp.After(st.consumedUpTo) {
		ok = false
	}
	if ok {
		st.consumedUpTo = decision.Timestamp
	}
	if !entries {
		return nil
	}

	in := policy.InputFromSnapshot(bar)
	in.InstrumentID = cfg.InstrumentID
	in.Symbol = cfg.Symbol
	in.Timestamp = bar.Timestamp
	in.Calibrated = true
	if ok {
		in.Decision = &decision
	}
	if s.portfolio != nil {
		s.portfolio.Candidate(bar)
	}
	sig := st.decideEntry(ctx, in)
	if sig.Direction == domain.JevDirectionNone {
		return nil
	}

	entryFill, ok := st.x.fill(bar, entrySide(sig.Direction))
	if !ok || !st.entrySessionOpen(bar.Timestamp) || bar.Timestamp.Before(st.cooldownUntil) {
		// 昼休み・立会時間外・引け前、または銘柄の損失後クールダウン中: live Enter
		// fails (ErrOutsideTradingSession / ErrSymbolInCooldown) and the
		// decision is spent.
		return nil
	}
	var quantity int64
	if s.portfolio != nil {
		if quantity = s.portfolio.Size(bar.Price); quantity == 0 {
			return nil
		}
		s.portfolio.Open(cfg.InstrumentID, sig.Direction, quantity, entryFill)
	}
	op := &openTrade{
		direction: sig.Direction, entryBar: bar, entryFill: entryFill, quantity: quantity,
		best: entryFill, lastFillIdx: idx,
		// sig is not NONE, so in.Decision is set.
		regime: in.Decision.MarketRegime.Regime, abArm: in.Decision.MarketRegime.ABArm,
	}
	op.trackBest(bar.Price)
	st.open = op
	s.openCount++
	return nil
}

// entrySessionOpen mirrors execution's sessionOpen beyond the fill phase: no
// new entry from the force-flat time on (it would be closed at once).
func (st *instState) entrySessionOpen(at time.Time) bool {
	closeAt, ok := st.x.closeAt(at)
	return !ok || at.Before(closeAt.Add(-st.cfg.Exit.ForceFlatBeforeClose))
}

// stepOpen evaluates the exit conditions of the open position on bars[idx]
// (execution's OnSnapshot): a triggered exit that cannot fill now (昼休み)
// is evaluated again on the next bar, and the VWAP baseline moves only when
// no exit was judged.
func (s *sim) stepOpen(ctx context.Context, st *instState, idx int) error {
	bar, op := st.bars[idx], st.open
	reason, exit, err := st.evaluateExit(ctx, idx)
	if err != nil {
		return fmt.Errorf("backtest: exit evaluation for %q at %s: %w", st.cfg.Symbol, bar.Timestamp, err)
	}
	fill, canFill := st.x.fill(bar, exitSide(op.direction))
	switch {
	case exit && canFill:
		s.closePosition(st, bar, reason, fill)
		return nil
	case !exit && bar.Feature.VWAP > 0:
		op.prev = &vwapcross.Observation{Price: bar.Price, VWAP: bar.Feature.VWAP}
	}
	if canFill {
		op.lastFillIdx, op.lastFill = idx, fill
	}
	op.trackBest(bar.Price)
	return nil
}

func (op *openTrade) trackBest(price float64) {
	if (op.direction == domain.JevDirectionShort && price < op.best) || (op.direction != domain.JevDirectionShort && price > op.best) {
		op.best = price
	}
}

// closePosition books the trade of st's open position, closed on bar at fill.
func (s *sim) closePosition(st *instState, bar domain.Snapshot, reason string, fill float64) {
	op := st.open
	trade := newTrade(op.entryBar, bar, op.direction, op.entryFill, fill, reason, st.cfg.Cost.FeeBps)
	trade.InstrumentID, trade.Symbol = st.cfg.InstrumentID, st.cfg.Symbol
	trade.Regime, trade.ABArm, trade.Quantity = op.regime, op.abArm, op.quantity
	s.trades = append(s.trades, trade)

	if s.portfolio != nil {
		s.portfolio.Close(st.cfg.InstrumentID, bar.Timestamp, float64(op.quantity)*op.entryFill*trade.NetReturnPct/100)
	}
	st.consumedUpTo = bar.Timestamp
	if trade.NetReturnPct < 0 {
		st.cooldownUntil = bar.Timestamp.Add(st.cfg.Exit.CooldownAfterLoss)
	}
	st.open = nil
	s.openCount--
}

// closeAtDataEnd closes every position still open when the bars ran out, at
// the last bar that could fill an exit (ExitReasonDataEnded). A position with
// no bar after its entry to hold through could not have been held, so it is
// not a trade (zero-return rows would dilute TradeCount/Expectancy/WinRate).
func (s *sim) closeAtDataEnd() {
	for _, st := range s.insts {
		op := st.open
		if op == nil {
			continue
		}
		if op.lastFill > 0 && st.bars[op.lastFillIdx].Timestamp.After(op.entryBar.Timestamp) {
			s.closePosition(st, st.bars[op.lastFillIdx], ExitReasonDataEnded, op.lastFill)
			continue
		}
		st.open = nil
		s.openCount--
	}
}
