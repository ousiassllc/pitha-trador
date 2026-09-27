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
// and the Scheduler's worker pool + full-scan/self-improve/log-rotation
// cron triggers (scheduler.Scheduler.Start), after first recovering any
// job left "running" by a previous crash (scheduler.Scheduler.Recover).
// All run until ctx is done or Stop is called. Neither cmd/desktop nor
// cmd/server calls this yet for the Scheduler/token-refresh goroutines
// (issue #51 wires the first such call site); the candidate-refresh
// ticker likewise only starts once Start itself is first called.
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
