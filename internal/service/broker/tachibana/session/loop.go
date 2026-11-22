package session

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/guard"
)

// supervise runs the session loop under the panic guard (FR-SCHED-6, issue
// #1211): a panic is logged and the loop starts again with a fresh login after
// a delay; once it panicked guard.MaxRestarts times in a row the
// session is stopped (status, notice, OnStop) and only the 立花 connection
// goes down. The logout at the end runs either way.
func (s *Session) supervise(ctx context.Context) {
	defer safego.Recover("tachibana session shutdown")
	gaveUp := guard.Supervise(ctx, s.clock, "tachibana session loop", func() { s.run(ctx) }, s.afterPanic)
	if gaveUp {
		s.stop(ctx)
	}
	s.logout()
	if gaveUp && s.cfg.OnStop != nil {
		s.cfg.OnStop()
	}
}

// afterPanic is the "restart" of the loop: whatever the panic interrupted, the
// next thing the loop does is a fresh login (the retry phase with no wait).
func (s *Session) afterPanic() {
	s.setPhase(phaseRetry)
	s.retryWait = 0
}

// stop records that the loop gave up: the status says so, the operator is
// told, and no login is made again until the process restarts.
func (s *Session) stop(ctx context.Context) {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	notify(ctx, s.cfg.Notifier, Notice{Kind: NoticeStopped, Message: guidanceStopped})
}

// Halt stops the session from outside, for the adapter when its EVENT feed
// gave up: the status and notice say the connection is stopped and no login
// is made again. It does not end the loop (cancel the Start context for that).
func (s *Session) Halt(ctx context.Context) {
	s.mu.Lock()
	already := s.stopped
	s.mu.Unlock()
	if !already {
		s.stop(ctx)
	}
}

// run is the session loop.
func (s *Session) run(ctx context.Context) {
	for ctx.Err() == nil {
		lost := s.sleep(ctx, s.waitFor())
		if ctx.Err() != nil {
			return
		}
		switch {
		case lost && s.getPhase() == phaseActive:
			s.onLost(ctx)
		case lost:
			// Nothing to renew: the session is already known to be down.
		case s.getPhase() == phaseActive:
			s.markClosed()
		default:
			_ = s.attempt(ctx) // the failure is recorded in the status and logged
		}
	}
}

// waitFor is how long the loop sleeps in the current phase.
func (s *Session) waitFor() time.Duration {
	now := s.clock.Now()
	switch s.getPhase() {
	case phaseActive:
		return tachibana.NextClose(now).Sub(now)
	case phaseClosed:
		return tachibana.NextClock(now, s.reauth).Sub(now)
	}
	return s.retryWait
}

// sleep waits d, returning true if a lost-session signal arrived first. It
// also returns when ctx ends.
func (s *Session) sleep(ctx context.Context, d time.Duration) (lost bool) {
	t := s.clock.NewTimer(max(d, 0))
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C():
	case <-s.lost:
		return true
	}
	return false
}

// markClosed ends the session at the 03:30 close: every virtual URL is dead
// and logging in works again only from 05:30.
func (s *Session) markClosed() {
	s.setPhase(phaseClosed)
	now := s.clock.Now()
	s.mu.Lock()
	s.status.Issue, s.status.Code = broker.SessionIssueOutOfHours, tachibana.ErrnoOutOfHours
	s.status.Guidance = guidanceClosed(s.reauth)
	s.status.Failures, s.status.Since = 0, time.Time{}
	s.status.NextReauth = tachibana.NextClock(now, s.reauth)
	s.mu.Unlock()
	s.client.SetOutOfHours(true)
	slog.Info("tachibana: broker closed (03:30), the session ends; re-login is scheduled", "reauth_at", tachibana.FormatClock(s.reauth))
}

// onLost handles a session the broker reported gone while the day's login
// should still hold (p_errno=2): re-login, but not in a tight loop, and flag
// a tool fighting over the same 認証ID.
func (s *Session) onLost(ctx context.Context) {
	now := s.clock.Now()
	if now.Sub(s.lastLogin) < contentionWindow {
		s.quickLosses++
	} else {
		s.quickLosses = 0
	}
	s.lossTimes = append(s.lossTimes, now)
	for len(s.lossTimes) > 0 && now.Sub(s.lossTimes[0]) > lossWindow {
		s.lossTimes = s.lossTimes[1:]
	}
	if s.quickLosses >= contentionLosses {
		s.mu.Lock()
		first := !s.contended
		s.contended = true
		s.mu.Unlock()
		if first {
			notify(ctx, s.cfg.Notifier, Notice{Kind: NoticeContention, Message: guidanceContention})
		}
	}
	if len(s.lossTimes) > maxLossRelogins {
		slog.Warn("tachibana: the session was lost too often, waiting for the next daily re-login",
			"losses", len(s.lossTimes), "window", lossWindow.String())
		s.setPhase(phaseClosed)
		s.mu.Lock()
		s.contended = true
		s.status.NextReauth = tachibana.NextClock(now, s.reauth)
		s.mu.Unlock()
		return
	}
	if wait := minReloginInterval - now.Sub(s.lastLogin); wait > 0 {
		if err := tachibana.Sleep(ctx, s.clock, wait); err != nil {
			return
		}
	}
	slog.Warn("tachibana: the broker ended the session (p_errno=2), logging in again")
	_ = s.attempt(ctx)
}

// attempt makes one login and moves the loop to the matching phase.
func (s *Session) attempt(ctx context.Context) error {
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		return errStopped
	}
	key, err := tachibana.LoadPrivateKey(s.cfg.KeyPath)
	var info tachibana.LoginInfo
	if err == nil {
		info, err = s.client.Login(ctx, s.cfg.AuthID, key)
	}
	s.observeNotices(ctx, info)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		s.failed(ctx, err)
		return err
	}
	s.succeeded()
	if s.cfg.OnLogin != nil {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			// The next login starts it again, so a give-up only logs.
			if guard.Supervise(ctx, s.clock, "tachibana on-login task", func() { s.cfg.OnLogin(ctx) }, func() {}) {
				slog.Error("tachibana: the on-login task keeps panicking; it runs again after the next login")
			}
		}()
	}
	return nil
}

func (s *Session) succeeded() {
	now := s.clock.Now()
	s.lastLogin = now
	s.setPhase(phaseActive)
	s.retryWait = backoffMin
	s.overdueNoted, s.docsNoted = false, false
	s.mu.Lock()
	s.status.Issue, s.status.Code, s.status.Guidance = broker.SessionIssueNone, 0, ""
	s.status.Failures, s.status.Since = 0, time.Time{}
	s.status.LoggedInAt = now
	s.status.NextReauth = tachibana.NextClock(tachibana.NextClose(now), s.reauth)
	s.status.DocumentsUnread = false
	s.mu.Unlock()
	slog.Info("tachibana: logged in", "next_reauth", tachibana.FormatClock(s.reauth),
		"valid_until", tachibana.NextClose(now).In(tachibana.JST).Format("01-02 15:04"))
}

func (s *Session) failed(ctx context.Context, err error) {
	now := s.clock.Now()
	f := classify(err)
	wasRetry := s.getPhase() == phaseRetry
	s.setPhase(phaseRetry)
	s.retryWait = s.nextRetryWait(now, f, wasRetry)

	s.mu.Lock()
	if s.status.Issue == f.issue && !s.status.Since.IsZero() {
		s.status.Failures++
	} else {
		s.status.Failures, s.status.Since = 1, now
	}
	s.status.Issue, s.status.Code, s.status.Guidance = f.issue, f.code, f.guidance
	s.status.DocumentsUnread = f.documentsUnread
	s.status.NextReauth = now.Add(s.retryWait)
	failures := s.status.Failures
	s.mu.Unlock()

	if f.issue == broker.SessionIssueOutOfHours {
		s.client.SetOutOfHours(true)
		slog.Info("tachibana: login refused while the broker is closed, waiting for it to open", "retry_in", s.retryWait.String())
	} else {
		slog.Warn("tachibana: login failed", "issue", string(f.issue), "code", f.code, "failures", failures, "retry_in", s.retryWait.String(), "error", err)
	}
	if f.documentsUnread && !s.docsNoted {
		s.docsNoted = true
		notify(ctx, s.cfg.Notifier, Notice{Kind: NoticeDocumentsUnread, Message: guidanceDocuments})
	}
	if !s.overdueNoted && tachibana.TimeOfDay(now) >= tachibana.LoginDeadline {
		s.overdueNoted = true
		notify(ctx, s.cfg.Notifier, Notice{Kind: NoticeLoginOverdue, Message: "立花証券 e支店APIへのログインが8:30までに成功していません。" + f.guidance})
	}
}

// nextRetryWait is the delay before the next login attempt: until the broker
// opens when it is closed, else an exponential backoff capped at backoffMax.
func (s *Session) nextRetryWait(now time.Time, f failure, wasRetry bool) time.Duration {
	if f.issue == broker.SessionIssueOutOfHours && tachibana.InClosedWindow(now) {
		return tachibana.NextClock(now, tachibana.OpenAt).Sub(now)
	}
	if !wasRetry || s.retryWait <= 0 {
		return backoffMin
	}
	return min(s.retryWait*2, backoffMax)
}

// logout ends the session when the loop stops, so no virtual URL outlives the
// process. It uses its own deadline: the loop's context is already done.
func (s *Session) logout() {
	ctx, cancel := context.WithTimeout(context.Background(), logoutTimeout)
	defer cancel()
	if err := s.client.Logout(ctx); err != nil {
		slog.Warn("tachibana: logout failed", "error", err)
		return
	}
	slog.Info("tachibana: logged out")
}
