package marketdatajob

import (
	"log/slog"
	"time"
)

// slowJobThreshold is the total HandleMarketData duration above which the
// per-phase breakdown is logged. A full scan runs one job per active
// instrument (~4,000) back to back, so a job near one second already pushes
// a cycle past its interval.
const slowJobThreshold = time.Second

// phaseTimer records how long each phase of one market-data job took so a
// slow job can be attributed to a phase (board fetch vs. DB vs. ...).
type phaseTimer struct {
	start, last time.Time
	attrs       []any
}

func newPhaseTimer() *phaseTimer {
	now := time.Now()
	return &phaseTimer{start: now, last: now}
}

// mark ends the current phase, recording its duration as "<name>_ms".
func (p *phaseTimer) mark(name string) {
	now := time.Now()
	p.attrs = append(p.attrs, name+"_ms", now.Sub(p.last).Milliseconds())
	p.last = now
}

// warnIfSlow logs the per-phase breakdown when the job took longer than
// slowJobThreshold. Phases not reached (an early error) are absent.
func (p *phaseTimer) warnIfSlow(symbol string) {
	total := time.Since(p.start)
	if total < slowJobThreshold {
		return
	}
	slog.Warn("marketdatajob: slow market-data job",
		append([]any{"symbol", symbol, "total_ms", total.Milliseconds()}, p.attrs...)...)
}
