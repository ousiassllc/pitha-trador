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
// (broker.Session.Start), the candidate-refresh ticker (issue #45),
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

	s.syncUniverse(ctx)

	if err := s.Broker.Start(ctx); err != nil {
		// The broker API being unreachable at startup (no broker app on a dev
		// machine, issue #44) must not stop the rest of the process: log and
		// continue without a session. Session.Start keeps retrying (issue
		// #295); the header banner shows the cause and every quote fetch fails
		// (broker.ErrNoSession or a request error) per job meanwhile.
		status := s.Broker.Status()
		slog.Error("bootstrap: broker initial session establishment failed, continuing without a session",
			"broker", s.Broker.Capabilities().Name, "issue", status.Issue, "guidance", status.Guidance, "error", err)
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
			Positions: s.Positions, Quotes: s.Broker, Exits: s.Execution,
			Open: marketcalendarOpen,
		}.Run(ctx, time.Duration(scan.HeldPositionIntervalSecondsMin)*time.Second, time.Duration(scan.HeldPositionIntervalSecondsMax)*time.Second)
	}()

	s.wg.Add(1) // startup symbol registration + PUSH subscription (flows.md §10.1)
	go func() { defer s.wg.Done(); s.Broker.Run(ctx) }()

	s.startRankingWatch(ctx)
	s.startRankingMeasure(ctx)

	if s.NewsIngestEnabled() {
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
