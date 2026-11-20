package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/dailyguardseed"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/heldposition"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/jobfailurelog"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/newswire"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/paperrisk"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/watchlistsize"
)

// Start launches every background goroutine this build's composition root owns: the broker session (broker.Session.Start) and PUSH
// subscription, the candidate-refresh ticker (issue #45), the Scheduler's worker pool + cron triggers (scheduler.Scheduler.Start; Recover
// first resets jobs a crash left "running"), the daily-guard and Paper capital seeding (dailyguardseed.Run #782, paperrisk.Seed #804), the
// once-a-minute failed-job summary by error code (jobfailurelog, #803), the trading-hours health alert check (#802), the MCP order-proposal
// processor (#828), and (with News Ingest) the disclosure skip record (#935). All run until ctx is done or Stop is called.
func (s *Services) Start(ctx context.Context) error {
	if _, err := s.Scheduler.Recover(ctx); err != nil {
		return fmt.Errorf("bootstrap: recover jobs: %w", err)
	}

	dailyguardseed.Run(ctx, s.Settings, time.Now())
	paperrisk.Seed(ctx, s.Settings, s.Risk, time.Now())
	s.aiRoles.Record(ctx) // which provider/model each AI role runs on since this start (issue #1080)
	s.syncUniverse(ctx)

	if err := s.Broker.Start(ctx); err != nil {
		// The broker API being unreachable at startup (no broker app on a dev
		// machine, issue #44) must not stop the rest of the process: log and
		// continue without a session. Session.Start keeps retrying (issue
		// #295); Home's 要対応 list shows the cause and every quote fetch fails
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

	s.proposalExec.Launch(ctx, &s.wg) // order proposals of the Paper results MCP (issue #828)
	s.startRankingWatch(ctx)
	watchlistsize.Start(ctx, &s.wg, s.watchListSizes, s.rankingWatcher != nil, s.watchlist.Len) // kabu only; read-only (issue #920)
	s.startRankingMeasure(ctx)
	s.startDailyBars(ctx)
	s.research.Start(ctx, &s.wg)
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.HealthAlerts.Run(ctx) }()
	s.startWatchList(ctx)
	jobfailurelog.Start(ctx, &s.wg, s.Jobs) // issue #803
	s.researchExtra.Start(ctx, &s.wg)       // replay evaluation (issue #1085): off by default

	if s.NewsIngestEnabled() {
		s.wg.Add(1)
		go newswire.RunPoll(ctx, &s.wg, s.News.Poll)
		s.disclosure.Start(ctx, &s.wg) // record only, on its own connection (issue #935)
	}

	return nil
}

// Stop stops the Scheduler's worker pool and cron triggers (scheduler.Scheduler.Stop), cancels the Jev shadow calls in flight (issue #872) and waits for the extra goroutines Start launched (tied to the ctx passed to Start: cancel it first, e.g. from OnShutdown) and the Scheduler to exit.
func (s *Services) Stop() {
	s.Scheduler.Stop()
	s.Trader.Close()
	s.wg.Wait()
}

// handleSelfImprove is the analytics queue Handler: the daily
// Continuous Loop batch Scheduler's post-close selfImproveCronSpec trigger
// enqueues (functional.md §4.14). It runs Governor.RunDaily over the
// recorded Calibration outcomes and logs what changed.
func (s *Services) handleSelfImprove(ctx context.Context, _ jobqueue.Job) error {
	result, err := s.Governor.RunDaily(ctx, s.Calibration)
	var proposalID int64
	if result.Proposal != nil {
		proposalID = result.Proposal.ID
	}
	slog.InfoContext(ctx, "bootstrap: self-improvement daily batch finished",
		"rolled_back_proposal_ids", result.RolledBack, "retried_applied_proposal_ids", result.RetriedApplied,
		"proposal_id", proposalID, "applied", result.Applied, "skipped_ai_stages", result.SkippedStages)
	if err != nil {
		return fmt.Errorf("bootstrap: self-improvement daily batch: %w", err)
	}
	return nil
}
