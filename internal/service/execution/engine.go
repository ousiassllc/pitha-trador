package execution

import (
	"errors"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// Errors Enter/Close return for an invalid request, distinguishable from
// the underlying repository errors they might otherwise be confused
// with.
var (
	// ErrDirectionInvalid is returned by Enter when
	// req.Signal.Direction is not domain.JevDirectionLong/Short (a NONE
	// signal never reaches Execution - Policy Engine only marks LONG/
	// SHORT signals risk_passed).
	ErrDirectionInvalid = errors.New("execution: signal direction must be LONG or SHORT")
	// ErrRiskNotPassed is returned by Enter when req.Signal.RiskPassed is
	// false: Execution only opens positions for signals Risk Engine
	// already approved (issue #37's "前提: #36").
	ErrRiskNotPassed = errors.New("execution: signal did not pass risk engine")
	// ErrPositionAlreadyOpen is returned by Enter when the instrument
	// already has an open position (positions_open_instrument_uq,
	// functional.md §4.9's per-symbol "position" state allows only one).
	ErrPositionAlreadyOpen = errors.New("execution: instrument already has an open position")
	// ErrSymbolInCooldown is returned by Enter when the symbol is still
	// within its post-loss cooldown window (Config.
	// CooldownAfterLossMinutes, functional.md §4.9 cooldown_until).
	ErrSymbolInCooldown = errors.New("execution: symbol is in post-loss cooldown")
	// ErrLimitPriceRequired is returned by Enter when the resolved order
	// type is LIMIT but req.LimitPrice is nil.
	ErrLimitPriceRequired = errors.New("execution: limit order requires a limit price")
)

// Engine implements Paper Trading Execution (functional.md §4.8, §4.9):
// Paper Entry/Exit against repository.OrderRepository/PositionRepository
// (entry.go/close.go), FR-EXIT-1's exit-condition evaluation
// (exitrule.go), and the per-symbol state read model (state.go).
type Engine struct {
	orders      *repository.OrderRepository
	positions   *repository.PositionRepository
	snapshots   *repository.SnapshotRepository
	decisions   *repository.DecisionRepository
	signals     *repository.SignalRepository
	instruments *repository.InstrumentRepository
	cfg         Config

	mu        sync.Mutex
	cooldowns map[string]time.Time // symbol -> cooldown_until (functional.md §4.9)
}

// Deps is every repository Engine reads/writes. Snapshots/Decisions/
// Signals/Instruments are used by EvaluateExit (trailing stop lookback,
// Jev direction/continuation_probability) and State (§4.9's per-symbol
// read model) - a caller that only needs Enter/Close (e.g. a unit test)
// may leave them nil; the methods that need them document the resulting
// no-op/degraded behavior.
type Deps struct {
	Orders      *repository.OrderRepository
	Positions   *repository.PositionRepository
	Snapshots   *repository.SnapshotRepository
	Decisions   *repository.DecisionRepository
	Signals     *repository.SignalRepository
	Instruments *repository.InstrumentRepository
}

// NewEngine returns an Engine backed by deps, applying cfg's documented
// defaults (Config.withDefaults) for every zero-valued field. It panics
// if deps.Orders or deps.Positions is nil - both are required for every
// Enter/Close call.
func NewEngine(deps Deps, cfg Config) *Engine {
	if deps.Orders == nil {
		panic("execution: NewEngine: deps.Orders is required")
	}
	if deps.Positions == nil {
		panic("execution: NewEngine: deps.Positions is required")
	}
	return &Engine{
		orders:      deps.Orders,
		positions:   deps.Positions,
		snapshots:   deps.Snapshots,
		decisions:   deps.Decisions,
		signals:     deps.Signals,
		instruments: deps.Instruments,
		cfg:         cfg.withDefaults(),
		cooldowns:   make(map[string]time.Time),
	}
}

// symbolCooldown reports whether symbol is still within a previously
// started cooldown at now, returning the cooldown's end time. Shared by
// entry.go's Enter (gate) and state.go's State (§4.9 cooldown_until
// display).
func (e *Engine) symbolCooldown(symbol string, now time.Time) (time.Time, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	until, ok := e.cooldowns[symbol]
	if !ok || !now.Before(until) {
		return time.Time{}, false
	}
	return until, true
}

// startCooldown is called by close.go's Close on a losing trade.
func (e *Engine) startCooldown(symbol string, from time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cooldowns[symbol] = from.Add(time.Duration(e.cfg.CooldownAfterLossMinutes) * time.Minute)
}
