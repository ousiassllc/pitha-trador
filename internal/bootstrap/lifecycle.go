package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Start launches every background goroutine this build's composition
// root owns: kabuステーションAPI token issuance/refresh
// (marketdata.Client.Start), the candidate-refresh ticker (issue #45),
// and the Scheduler's worker pool + full-scan/self-improve/
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
		// with no token. Every subsequent GetBoard call simply fails
		// (ErrNoToken or a request error) and handleMarketData's own
		// per-job error handling already covers that.
		slog.Error("bootstrap: kabuステーションAPI initial token issuance failed, continuing without a token", "error", err)
	}

	fullScanInterval := time.Duration(s.strategy.Scan.FullScanIntervalSeconds) * time.Second
	if err := s.Scheduler.Start(ctx, fullScanInterval); err != nil {
		return fmt.Errorf("bootstrap: start scheduler: %w", err)
	}

	s.wg.Add(1)
	go s.candidateRefreshTicker(ctx)

	if s.newsEnabled {
		s.wg.Add(1)
		go s.newsIngestTicker(ctx)
	}

	return nil
}

// Stop stops the Scheduler's worker pool and cron triggers
// (scheduler.Scheduler.Stop) and waits for every extra goroutine Start
// launched (currently: candidateRefreshTicker) to exit. Those extra
// goroutines are tied to the ctx passed to Start, not to this method:
// callers cancel that same ctx (e.g. via context.WithCancel in
// main/OnStartup, canceled from OnShutdown) to stop them, then call Stop
// to block until they (and the Scheduler) have actually exited.
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

	ticker := time.NewTicker(newsPollInterval)
	defer ticker.Stop()
	for {
		if err := s.News.Poll(ctx); err != nil && ctx.Err() == nil {
			slog.Error("bootstrap: news ingest cycle failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
