package dailybars

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

// checkpointEvery is how many symbols pass between two saves of the run row,
// so a crash resumes near where it stopped.
const checkpointEvery = 100

// batch is the walk over one night's universe. The nightly Runner and the
// research collection (Research) share it; they differ only in where they
// resume (remaining), what they record per symbol (attempted) and whether the
// closed window 03:30〜05:30 also stops them.
type batch struct {
	name     string // "nightly" / "research", in logs and error texts
	source   Source
	bars     Bars
	runs     Runs
	settings tachibanasource.TachibanaNightlySettings
	clock    tachibana.Clock

	// remaining narrows the sorted universe to the symbols still to request,
	// given the run so far (its Cursor, RunDate).
	remaining func(ctx context.Context, run domain.DailyBarRun, symbols []string) ([]string, error)
	// attempted, when set, runs after each symbol whose request was answered
	// (err is the failure of that symbol, nil on success; saved the bars written).
	attempted func(ctx context.Context, run domain.DailyBarRun, symbol string, saved int, err error) error
	// stopInClosedWindow also stops the walk from 03:30 to 05:30, when the
	// broker is closed and refuses logins.
	stopInClosedWindow bool
}

// night walks the universe from prev's resume point and returns the stored
// run. A night ends one of three ways: every symbol was attempted (succeeded,
// unless all of them failed), or it was cut short (failed, with the cursor at
// the last symbol done): ctx ended, the broker has no session (閉局・失効),
// the clock reached the daytime window, or a store failed.
func (b *batch) night(ctx context.Context, prev domain.DailyBarRun) domain.DailyBarRun {
	begun := b.clock.Now()
	run := prev
	run.Status = domain.DailyBarRunRunning
	run.Error = ""
	if prev.StartedAt.IsZero() {
		run.StartedAt = begun
	}
	run.FinishedAt = nil
	baseMS := prev.DurationMS
	elapse := func() { run.DurationMS = baseMS + b.clock.Now().Sub(begun).Milliseconds() }

	finish := func(status, reason string) domain.DailyBarRun {
		elapse()
		run.Status, run.Error = status, reason
		done := b.clock.Now()
		run.FinishedAt = &done
		// ctx may already be canceled (shutdown): the row is still written.
		if err := b.runs.Save(context.WithoutCancel(ctx), run); err != nil {
			slog.Error("tachibana: save the "+b.name+" daily bar run", "run_date", run.RunDate, "error", err)
		}
		return run
	}

	targets, err := b.source.DailyBarTargets(ctx)
	if err != nil {
		return finish(domain.DailyBarRunFailed, fmt.Sprintf("銘柄マスタを取得できません: %v", err))
	}
	symbols := universe(targets, b.settings)
	run.Symbols = len(symbols)
	if symbols, err = b.remaining(ctx, run, symbols); err != nil {
		return finish(domain.DailyBarRunFailed, fmt.Sprintf("進み具合を読めません: %v", err))
	}
	slog.Info("tachibana: "+b.name+" daily bars started", "run_date", run.RunDate, "symbols", run.Symbols,
		"remaining", len(symbols), "max_per_second", b.settings.MaxPerSecond)
	if err := b.runs.Save(ctx, run); err != nil {
		return finish(domain.DailyBarRunFailed, fmt.Sprintf("実行記録を保存できません: %v", err))
	}

	interval := b.interval()
	var lastSend time.Time
	for i, symbol := range symbols {
		if reason := b.stopReason(ctx); reason != "" {
			return finish(domain.DailyBarRunFailed, reason)
		}
		if !lastSend.IsZero() {
			if err := tachibana.Sleep(ctx, b.clock, interval-b.clock.Now().Sub(lastSend)); err != nil {
				return finish(domain.DailyBarRunFailed, "停止しました")
			}
			if reason := b.stopReason(ctx); reason != "" { // the wait may have crossed 8:00
				return finish(domain.DailyBarRunFailed, reason)
			}
		}
		lastSend = b.clock.Now()
		bars, err := b.source.DailyBars(ctx, symbol)
		run.Requests++
		saved := 0
		switch {
		case err == nil:
			if saved, err = b.store(ctx, symbol, bars); err != nil {
				return finish(domain.DailyBarRunFailed, fmt.Sprintf("日足を保存できません（%s）: %v", symbol, err))
			}
			run.SavedBars += saved
		case sessionLost(err) || ctx.Err() != nil:
			run.Requests-- // nothing was answered; the symbol is retried
			return finish(domain.DailyBarRunFailed, fmt.Sprintf("セッションがありません・停止しました: %v", err))
		default:
			run.Failed++
			slog.Debug("tachibana: "+b.name+" daily bar request failed", "symbol", symbol, "error", err)
		}
		run.Cursor = symbol
		if b.attempted != nil {
			if aerr := b.attempted(ctx, run, symbol, saved, err); aerr != nil {
				return finish(domain.DailyBarRunFailed, fmt.Sprintf("進み具合を保存できません（%s）: %v", symbol, aerr))
			}
		}
		if (i+1)%checkpointEvery == 0 {
			elapse()
			if err := b.runs.Save(ctx, run); err != nil {
				return finish(domain.DailyBarRunFailed, fmt.Sprintf("実行記録を保存できません: %v", err))
			}
		}
	}

	if run.Symbols > 0 && run.Failed == run.Symbols {
		return finish(domain.DailyBarRunFailed, "すべての銘柄の日足取得に失敗しました")
	}
	done := finish(domain.DailyBarRunSucceeded, "")
	slog.Info("tachibana: "+b.name+" daily bars finished", "run_date", done.RunDate, "symbols", done.Symbols,
		"requests", done.Requests, "saved_bars", done.SavedBars, "failed", done.Failed, "duration_ms", done.DurationMS)
	return done
}

// afterCursor resumes a night after the last symbol its run row recorded, in
// the sorted order the first attempt used.
func afterCursor(_ context.Context, run domain.DailyBarRun, symbols []string) ([]string, error) {
	return symbols[sort.SearchStrings(symbols, run.Cursor+"\x00"):], nil
}

// stopReason is why the batch must not send another request now ("" = go on):
// ctx ended, or the clock is in the 8:00〜15:30 daytime window (#720), or, for
// a batch that logs in itself, in the closed window 03:30〜05:30.
func (b *batch) stopReason(ctx context.Context) string {
	now := b.clock.Now()
	switch {
	case ctx.Err() != nil:
		return "停止しました"
	case tachibana.InDaytime(now):
		return "日中（8:00〜15:30）のため中断しました"
	case b.stopInClosedWindow && tachibana.InClosedWindow(now):
		return "閉局（3:30〜5:30）のため中断しました"
	}
	return ""
}

// interval is the shortest spacing between two request starts (the Settings'
// per-second rate; the adapter queue enforces its own, global, budget too).
func (b *batch) interval() time.Duration {
	rate := b.settings.MaxPerSecond
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
func (b *batch) store(ctx context.Context, symbol string, bars []domain.DailyBar) (int, error) {
	if len(bars) == 0 {
		return 0, nil
	}
	last, err := b.bars.Latest(ctx, symbol)
	if err != nil {
		return 0, err
	}
	if last == nil {
		return b.bars.Save(ctx, symbol, bars, false)
	}
	fresh := bars[:0:0]
	for _, bar := range bars {
		switch {
		case bar.TradeDate > last.TradeDate:
			fresh = append(fresh, bar)
		case bar.TradeDate == last.TradeDate && !bar.SameAdjustment(*last):
			return b.bars.Save(ctx, symbol, bars, true)
		}
	}
	return b.bars.Save(ctx, symbol, fresh, false)
}
