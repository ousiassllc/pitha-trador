package dailybars

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

// checkpointEvery is how many symbols pass between two saves of the run row,
// so a crash resumes near where it stopped.
const checkpointEvery = 100

// night walks the universe from prev's cursor and returns the stored run. A
// night ends one of three ways: every symbol was attempted (succeeded, unless
// all of them failed), or it was cut short (failed, with the cursor at the
// last symbol done): ctx ended, the broker has no session (閉局・失効), the
// clock reached the daytime window, or a store failed.
func (r *Runner) night(ctx context.Context, prev domain.DailyBarRun) domain.DailyBarRun {
	begun := r.clock.Now()
	run := prev
	run.Status = domain.DailyBarRunRunning
	run.Error = ""
	if prev.StartedAt.IsZero() {
		run.StartedAt = begun
	}
	run.FinishedAt = nil
	baseMS := prev.DurationMS
	elapse := func() { run.DurationMS = baseMS + r.clock.Now().Sub(begun).Milliseconds() }

	finish := func(status, reason string) domain.DailyBarRun {
		elapse()
		run.Status, run.Error = status, reason
		done := r.clock.Now()
		run.FinishedAt = &done
		// ctx may already be canceled (shutdown): the row is still written.
		if err := r.cfg.Runs.Save(context.WithoutCancel(ctx), run); err != nil {
			slog.Error("tachibana: save the nightly daily bar run", "run_date", run.RunDate, "error", err)
		}
		return run
	}

	targets, err := r.cfg.Source.DailyBarTargets(ctx)
	if err != nil {
		return finish(domain.DailyBarRunFailed, fmt.Sprintf("銘柄マスタを取得できません: %v", err))
	}
	symbols := universe(targets, r.cfg.Settings)
	run.Symbols = len(symbols)
	// A symbol after the cursor, in the sorted order the first attempt used.
	symbols = symbols[sort.SearchStrings(symbols, run.Cursor+"\x00"):]
	slog.Info("tachibana: nightly daily bars started", "run_date", run.RunDate, "symbols", run.Symbols,
		"remaining", len(symbols), "max_per_second", r.cfg.Settings.MaxPerSecond)
	if err := r.cfg.Runs.Save(ctx, run); err != nil {
		return finish(domain.DailyBarRunFailed, fmt.Sprintf("実行記録を保存できません: %v", err))
	}

	interval := r.interval()
	var lastSend time.Time
	for i, symbol := range symbols {
		if reason := r.stopReason(ctx); reason != "" {
			return finish(domain.DailyBarRunFailed, reason)
		}
		if !lastSend.IsZero() {
			if err := tachibana.Sleep(ctx, r.clock, interval-r.clock.Now().Sub(lastSend)); err != nil {
				return finish(domain.DailyBarRunFailed, "停止しました")
			}
			if reason := r.stopReason(ctx); reason != "" { // the wait may have crossed 8:00
				return finish(domain.DailyBarRunFailed, reason)
			}
		}
		lastSend = r.clock.Now()
		bars, err := r.cfg.Source.DailyBars(ctx, symbol)
		run.Requests++
		switch {
		case err == nil:
			saved, err := r.store(ctx, symbol, bars)
			if err != nil {
				return finish(domain.DailyBarRunFailed, fmt.Sprintf("日足を保存できません（%s）: %v", symbol, err))
			}
			run.SavedBars += saved
		case sessionLost(err) || ctx.Err() != nil:
			run.Requests-- // nothing was answered; the symbol is retried
			return finish(domain.DailyBarRunFailed, fmt.Sprintf("セッションがありません・停止しました: %v", err))
		default:
			run.Failed++
			slog.Debug("tachibana: nightly daily bar request failed", "symbol", symbol, "error", err)
		}
		run.Cursor = symbol
		if (i+1)%checkpointEvery == 0 {
			elapse()
			if err := r.cfg.Runs.Save(ctx, run); err != nil {
				return finish(domain.DailyBarRunFailed, fmt.Sprintf("実行記録を保存できません: %v", err))
			}
		}
	}

	if run.Symbols > 0 && run.Failed == run.Symbols {
		return finish(domain.DailyBarRunFailed, "すべての銘柄の日足取得に失敗しました")
	}
	done := finish(domain.DailyBarRunSucceeded, "")
	slog.Info("tachibana: nightly daily bars finished", "run_date", done.RunDate, "symbols", done.Symbols,
		"requests", done.Requests, "saved_bars", done.SavedBars, "failed", done.Failed, "duration_ms", done.DurationMS)
	return done
}

// stopReason is why the batch must not send another request now ("" = go on):
// ctx ended, or the clock is in the 8:00〜15:30 daytime window (#720).
func (r *Runner) stopReason(ctx context.Context) string {
	switch {
	case ctx.Err() != nil:
		return "停止しました"
	case tachibana.InDaytime(r.clock.Now()):
		return "日中（8:00〜15:30）のため中断しました"
	}
	return ""
}

// interval is the shortest spacing between two request starts (the Settings'
// per-second rate; the adapter queue enforces its own, global, budget too).
func (r *Runner) interval() time.Duration {
	rate := r.cfg.Settings.MaxPerSecond
	if rate <= 0 {
		rate = 1
	}
	return time.Duration(float64(time.Second) / rate)
}

// sessionLost reports a failure that ends the night rather than one symbol:
// no session, a lost virtual URL or the 閉局.
func sessionLost(err error) bool {
	var apiErr *tachibana.APIError
	if errors.Is(err, broker.ErrNoSession) {
		return true
	}
	if errors.As(err, &apiErr) {
		return apiErr.Kind() == tachibana.KindSessionExpired || apiErr.Kind() == tachibana.KindOutOfHours
	}
	return false
}

// store saves the bars of symbol that are not stored yet and returns how many
// it wrote. With nothing stored it keeps the whole history. Otherwise it
// keeps the days after the latest stored one, unless the broker's adjusted
// values of that latest day changed (a 株式分割 rescales the whole history):
// then the stored bars are replaced by the fresh history.
func (r *Runner) store(ctx context.Context, symbol string, bars []domain.DailyBar) (int, error) {
	if len(bars) == 0 {
		return 0, nil
	}
	last, err := r.cfg.Bars.Latest(ctx, symbol)
	if err != nil {
		return 0, err
	}
	if last == nil {
		return r.cfg.Bars.Save(ctx, symbol, bars, false)
	}
	fresh := bars[:0:0]
	for _, b := range bars {
		switch {
		case b.TradeDate > last.TradeDate:
			fresh = append(fresh, b)
		case b.TradeDate == last.TradeDate && !b.SameAdjustment(*last):
			return r.cfg.Bars.Save(ctx, symbol, bars, true)
		}
	}
	return r.cfg.Bars.Save(ctx, symbol, fresh, false)
}
