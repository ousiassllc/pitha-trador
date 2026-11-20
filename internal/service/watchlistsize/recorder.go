// Package watchlistsize keeps the daily size of the runtime watch list
// (issue #920): while the market is open it reads how many symbols the
// in-memory watch list holds (rankingwatch.Watchlist.Len, a read-only count)
// once a minute and stores that count with its source in
// watch_list_runtime_days (migration 000070). It never stores a symbol and
// takes no part in the ranking fetch, the Scout input, the broker selection or
// the watch list generation: it only observes, and a failed write is logged and
// retried at the next tick, never a reason to stop trading.
package watchlistsize

import (
	"context"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// SourceRanking is the source of the kabu ranking-driven watch list.
const SourceRanking = "ranking"

// CheckInterval is the default time between observations: how often the Recorder observes the watch list size.
const CheckInterval = time.Minute

// Store persists one observation (watchlistruntime.Repository).
type Store interface {
	RecordRuntimeSize(ctx context.Context, date, source string, size int, now time.Time) error
}

// Config wires a Recorder. Now defaults to time.Now.
type Config struct {
	Store  Store
	Source string
	// Size is the current number of watched symbols (a count, no codes).
	Size func() int
	Now  func() time.Time
	// Interval defaults to CheckInterval (tests shorten it).
	Interval time.Duration
}

// Recorder samples the runtime watch list size during the TSE session.
type Recorder struct{ cfg Config }

// NewRecorder returns a Recorder.
func NewRecorder(cfg Config) *Recorder {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Interval <= 0 {
		cfg.Interval = CheckInterval
	}
	return &Recorder{cfg: cfg}
}

// Check runs one observation: outside the trading session it does nothing
// (the watch list keeps the last list then, which is not new information).
func (r *Recorder) Check(ctx context.Context) error {
	now := r.cfg.Now()
	if !marketcalendar.TSE.IsOpen(now) {
		return nil
	}
	date := now.In(marketcalendar.JST).Format(time.DateOnly)
	return r.cfg.Store.RecordRuntimeSize(ctx, date, r.cfg.Source, r.cfg.Size(), now)
}

// Start runs a Recorder of size in the background (wg tracks it) when enabled;
// size is a count, read once per pass. The caller passes enabled=false for a
// build without the kabu runtime watch list, which is then left alone.
func Start(ctx context.Context, wg *sync.WaitGroup, store Store, enabled bool, size func() int) {
	if !enabled {
		return
	}
	rec := NewRecorder(Config{Store: store, Source: SourceRanking, Size: size})
	wg.Add(1)
	go func() { defer wg.Done(); rec.Run(ctx) }()
}

// Run observes every CheckInterval until ctx is done. A panic or error in a
// pass is logged by safego.Loop and the next pass runs as usual.
func (r *Recorder) Run(ctx context.Context) {
	safego.Loop(ctx, "watch list size record", func() time.Duration { return r.cfg.Interval }, r.Check)
}
