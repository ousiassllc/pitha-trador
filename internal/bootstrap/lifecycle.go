package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/heldposition"
	"github.com/ousiassllc/pitha-trador/internal/safego"
)

// Start launches every background goroutine this build's composition
// root owns: kabuステーションAPI token issuance/refresh
// (marketdata.Client.Start), the candidate-refresh ticker (issue #45),
// the PUSH subscription (pushfeed), and the Scheduler's worker pool + full-scan/self-improve/
// outcome-labeling/operator-heartbeat/log-rotation cron triggers
// (scheduler.Scheduler.Start), after first recovering any job left
// "running" by a previous crash (scheduler.Scheduler.Recover). All run
// until ctx is done or Stop is called. cmd/desktop calls it from Wails'
// OnStartup and cmd/server from main, each stopping it on shutdown.
func (s *Services) Start(ctx context.Context) error {
	if _, err := s.Scheduler.Recover(ctx); err != nil {
		return fmt.Errorf("bootstrap: recover jobs: %w", err)
	}

	if err := s.MarketData.Start(ctx, defaultTokenRefreshInterval); err != nil {
		// kabuステーションAPI not reachable at startup (dev machine
		// without the kabuステーションアプリ running, issue #44's own
		// scope note) must not prevent the rest of the process (Scanner
		// Dashboard, API, other queues) from starting: log and continue
		// with no token. MarketData.Start keeps retrying in the background
		// (issue #295) and the header banner (`/system/marketdata-status`)
		// tells the operator the cause. Every GetBoard call until a token
		// is obtained simply fails (ErrNoToken or a request error) and
		// marketdatajob.Handler's own per-job error handling already
		// covers that.
		status := s.MarketData.TokenStatus()
		slog.Error("bootstrap: kabuステーションAPI initial token issuance failed, continuing without a token",
			"issue", status.Issue, "guidance", status.Guidance(), "error", err)
	}

	fullScanInterval := time.Duration(s.strategy.Scan.FullScanIntervalSeconds) * time.Second
	if err := s.Scheduler.Start(ctx, fullScanInterval); err != nil {
		return fmt.Errorf("bootstrap: start scheduler: %w", err)
	}

	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.candidates.Run(ctx) }()

	s.wg.Add(1)
	go func() { // FR-SCHED-4 / issue #156
		defer s.wg.Done()
		scan := s.strategy.Scan
		heldposition.Monitor{
			Positions: s.Positions, Boards: s.MarketData, Exits: s.Execution,
			Exchange: defaultKabuExchange, Open: marketcalendarOpen,
		}.Run(ctx, time.Duration(scan.HeldPositionIntervalSecondsMin)*time.Second, time.Duration(scan.HeldPositionIntervalSecondsMax)*time.Second)
	}()

	s.wg.Add(1) // startup symbol registration + PUSH subscription (flows.md §10.1)
	go func() { defer s.wg.Done(); s.PushFeed.Run(ctx) }()

	if s.newsEnabled {
		s.wg.Add(1)
		go s.newsIngestTicker(ctx)
	}

	return nil
}

// Stop stops the Scheduler's worker pool and cron triggers
// (scheduler.Scheduler.Stop) and waits for every extra goroutine Start
// launched (the candidate-refresh ticker, held-position monitor, PUSH
// feed, news ingest) to exit. Those extra goroutines are tied to the ctx
// passed to Start, not to this method: callers cancel that same ctx (e.g.
// via context.WithCancel in main/OnStartup, canceled from OnShutdown) to
// stop them, then call Stop to block until they (and the Scheduler) have
// actually exited.
func (s *Services) Stop() {
	s.Scheduler.Stop()
	s.wg.Wait()
}

// newsIngestTicker runs one News Ingest cycle (newsfeed.Service.Poll)
// every newsPollInterval until ctx is done, starting with an immediate
// cycle (FR-LUNA-1). A failed cycle is logged and the ticker carries on:
// News Ingest problems must never affect the rest of the system
// (FR-LUNA-4).
func (s *Services) newsIngestTicker(ctx context.Context) {
	defer s.wg.Done()

	wait := time.Duration(0) // the first cycle runs immediately
	safego.Loop(ctx, "news ingest", func() time.Duration {
		d := wait
		wait = newsPollInterval
		return d
	}, s.News.Poll)
}
