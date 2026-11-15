// Package researchquotes records, once a minute while the market is open, what
// 立花 e支店 quotes for the symbols kabu watches (plus a few more), next to the
// kabu broker's own market_snapshots (issue #1083): research_quote_samples, so
// the two sources can be compared (MCP research_quote_source_compare).
//
// It is a research-only side channel and trades nothing: it writes only
// research_quote_samples and the quotes row of research_tachibana_status, never
// market_snapshots, and nothing it records reaches the Feature Engine, Scout,
// Trader, Paper, Policy, Risk or any order path. It never changes how kabu
// chooses its watch symbols (KabuSymbols only reads them), and the broker
// selection is untouched: it runs only when 立花 is not the selected broker.
//
// The switch (research.tachibana_quotes.enabled) is OFF by default and read
// every cycle. While it is off, outside the day's window, or while the
// credentials are unset, nothing is acquired and nothing is sent: Start (which
// logs in and launches the EVENT feed) is first called at 08:50 JST of a
// trading day with the switch on, and the feed is the research feed's own
// (one 認証ID, one session: the parent shares it through researchhub). It is
// fed by the EVENT stream; the feed's rate-limited REST refill (Feed.Latest)
// assists only for a symbol that has no value at all, and the feed itself
// enforces the daily connection budget and the REST interval.
package researchquotes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchquote"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchstatus"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/event"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

var (
	// ErrNotConfigured is what Config.Start returns when the 認証ID or the
	// 秘密鍵 path is unset: the status becomes not_configured and nothing is sent.
	ErrNotConfigured = errors.New("researchquotes: 立花の認証ID または 秘密鍵のパスが未設定です")
	// ErrBrokerIsTachibana is what Config.Start returns when 立花 is the selected
	// broker, which owns the session: the status becomes broker_is_tachibana.
	ErrBrokerIsTachibana = errors.New("researchquotes: ブローカーが立花のため、研究用の時価の記録は動きません")
)

const (
	// acquireFrom is when, on a trading day, the feed is started and the
	// symbols are subscribed (minutes after JST midnight: 08:50), so the EVENT
	// stream is connected before the 9:00 open.
	acquireFrom = 8*60 + 50
	// tickOffset is where in each minute the recorder runs (the 5th second, so
	// the minute's last notifications are in).
	tickOffset = 5 * time.Second
)

// Feed is the part of the 立花 EVENT feed (*event.Feed) the recorder uses.
type Feed interface {
	// SetWatch sets the symbols to receive; the feed changes its subscription
	// only by one batched reconnect for new symbols, within its daily budget.
	SetWatch(ctx context.Context, symbols []string) error
	// Peek is the latest merged value whatever its age (no request).
	Peek(symbol string) (q broker.Quote, updatedAt time.Time, ok bool)
	// ViaREST reports whether Peek's value was last updated by a REST refill.
	ViaREST(symbol string) bool
	// Latest is the EVENT value if fresh, else the feed's REST refill (at most
	// once per REST interval).
	Latest(ctx context.Context, symbol string) (broker.Quote, error)
	ConnectionsToday() int
}

var _ Feed = (*event.Feed)(nil)

// Store writes the samples (*researchquote.Repository).
type Store interface {
	InsertBatch(ctx context.Context, samples []researchquote.Sample) (int, error)
}

// Status records the recorder's state (*researchstatus.Repository).
type Status interface {
	Set(ctx context.Context, component, state, detail string, now time.Time) error
}

// Config wires a Recorder. Every field but KabuSymbols, ScreenSymbols, InSession
// and Clock is required.
type Config struct {
	// Settings reads the effective settings; it is called every cycle, so a
	// saved change applies from the next minute.
	Settings func(ctx context.Context) (tachibanasource.TachibanaSourceSettings, error)
	// Start returns the research feed, starting it (login, EVENT) on the first
	// call. It returns ErrNotConfigured or ErrBrokerIsTachibana (wrapped is
	// fine) when the research feed cannot run, without any network access.
	Start func(ctx context.Context) (Feed, error)
	// KabuSymbols are the symbols kabu watches now, in order; nil: none. It only reads.
	KabuSymbols func() []string
	// ScreenSymbols are the daily-screen top symbols for the day (ScreenSymbols);
	// nil: the daily_screen source adds nothing.
	ScreenSymbols func(ctx context.Context, settings tachibanasource.TachibanaSourceSettings, now time.Time) ([]string, error)
	Store         Store
	Status        Status
	// InSession reports whether the market is in session at t; default
	// marketcalendar.TSE.IsOpen. Samples are recorded only then.
	InSession func(t time.Time) bool
	// Clock overrides the wall clock (tests).
	Clock tachibana.Clock
}

// Recorder is the minute-by-minute 時価 recorder. Run and Tick must not run
// concurrently.
type Recorder struct {
	cfg   Config
	clock tachibana.Clock

	feed    Feed
	plan    planner
	watched []string // what SetWatch was last given

	extrasKey string // what extras was computed for (daily_screen: per JST day)
	extras    []string
}

// New returns a Recorder; Run starts it.
func New(cfg Config) *Recorder {
	if cfg.InSession == nil {
		cfg.InSession = marketcalendar.TSE.IsOpen
	}
	return &Recorder{cfg: cfg, clock: tachibana.OrReal(cfg.Clock)}
}

// Run ticks at the 5th second of every minute until ctx is done. A tick's
// failure or panic is logged and never stops the loop, nor the process that
// also trades.
func (r *Recorder) Run(ctx context.Context) {
	for {
		now := r.clock.Now()
		next := now.Truncate(time.Minute).Add(tickOffset)
		if !next.After(now) {
			next = next.Add(time.Minute)
		}
		t := r.clock.NewTimer(next.Sub(now))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C():
		}
		r.safeTick(ctx)
	}
}

func (r *Recorder) safeTick(ctx context.Context) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("researchquotes: panic while recording 時価", "panic", fmt.Sprint(p))
		}
	}()
	if err := r.Tick(ctx, r.clock.Now()); err != nil && ctx.Err() == nil {
		slog.Warn("researchquotes: 時価の記録に失敗しました", "error", err)
	}
}

// Tick is one cycle at now: it follows the switch and the day's window, plans
// the symbols and, in session, records the minute of now.
func (r *Recorder) Tick(ctx context.Context, now time.Time) error {
	set, err := r.cfg.Settings(ctx)
	if err != nil {
		return fmt.Errorf("researchquotes: settings: %w", err)
	}
	research := set.Research
	switch {
	case !research.QuotesEnabled:
		r.status(ctx, researchstatus.StateDisabled, "研究用の時価の記録は OFF です", now)
		return nil
	case !r.inWindow(now):
		r.status(ctx, researchstatus.StateWaiting, "立会日の 08:50（JST）から 15:30 まで動きます。いまは待機中です", now)
		return nil
	}
	feed, ok, err := r.ensureFeed(ctx, now)
	if !ok {
		return err
	}
	symbols, err := r.planSymbols(ctx, set, now)
	if err != nil {
		return err
	}
	if !slices.Equal(symbols, r.watched) {
		if err := feed.SetWatch(ctx, symbols); err != nil {
			r.status(ctx, researchstatus.StateError, "立花の購読銘柄を設定できませんでした", now)
			return fmt.Errorf("researchquotes: set watch: %w", err)
		}
		r.watched = slices.Clone(symbols)
	}
	switch {
	case len(symbols) == 0:
		r.status(ctx, researchstatus.StateWaiting, "記録する銘柄がまだありません（kabu の監視銘柄も追加の銘柄も空）", now)
		return nil
	case !r.cfg.InSession(now):
		r.status(ctx, researchstatus.StateOK, fmt.Sprintf("準備済み: 銘柄 %d。立会中に記録します", len(symbols)), now)
		return nil
	}
	return r.record(ctx, now, feed, symbols)
}

// inWindow is whether now is on a trading day between 08:50 and the 15:30 close (JST).
func (r *Recorder) inWindow(now time.Time) bool {
	closeAt, ok := marketcalendar.TSE.CloseAt(now)
	if !ok {
		return false
	}
	j := now.In(tachibana.JST)
	return j.Hour()*60+j.Minute() >= acquireFrom && now.Before(closeAt)
}

// ensureFeed starts the feed on first need. ok is false when the recorder
// cannot go on this cycle (the status says why; err is non-nil for a failure
// rather than a normal not-running state).
func (r *Recorder) ensureFeed(ctx context.Context, now time.Time) (Feed, bool, error) {
	if r.feed != nil {
		return r.feed, true, nil
	}
	feed, err := r.cfg.Start(ctx)
	switch {
	case errors.Is(err, ErrNotConfigured):
		r.status(ctx, researchstatus.StateNotConfigured, "立花の認証ID または 秘密鍵のパスが未設定のため、記録しません", now)
		return nil, false, nil
	case errors.Is(err, ErrBrokerIsTachibana):
		r.status(ctx, researchstatus.StateBrokerIsTachibana, "ブローカーが立花のため、研究用の時価の記録は動きません", now)
		return nil, false, nil
	case err != nil:
		r.status(ctx, researchstatus.StateError, "立花の時価の受信を始められませんでした", now)
		return nil, false, fmt.Errorf("researchquotes: start feed: %w", err)
	}
	r.feed = feed
	return feed, true, nil
}

// planSymbols is the day's sticky symbol plan: kabu's symbols, then the extras
// of the configured source.
func (r *Recorder) planSymbols(ctx context.Context, set tachibanasource.TachibanaSourceSettings, now time.Time) ([]string, error) {
	research := set.Research
	day := now.In(tachibana.JST).Format(time.DateOnly)
	var kabu []string
	if r.cfg.KabuSymbols != nil {
		kabu = r.cfg.KabuSymbols()
	}
	extras, err := r.extrasFor(ctx, set, day, now)
	if err != nil {
		return nil, err
	}
	return slices.Clone(r.plan.update(day, kabu, extras, research.QuotesMaxSymbols)), nil
}

// extrasFor is the symbols added to kabu's: the manual list, or the daily
// screen's top (computed once per JST day: the bars it ranks change nightly).
func (r *Recorder) extrasFor(ctx context.Context, set tachibanasource.TachibanaSourceSettings, day string, now time.Time) ([]string, error) {
	research := set.Research
	if research.QuotesExtraSource != tachibanasource.ResearchExtraDailyScreen || r.cfg.ScreenSymbols == nil {
		return research.QuotesExtraSymbols, nil
	}
	key := day + "|" + fmt.Sprint(set.Screen) + "|" + strings.Join(set.Nightly.ExcludeSymbols, ",")
	if key != r.extrasKey {
		picked, err := r.cfg.ScreenSymbols(ctx, set, now)
		if err != nil {
			return nil, fmt.Errorf("researchquotes: daily screen: %w", err)
		}
		r.extrasKey, r.extras = key, picked
	}
	return r.extras, nil
}

func (r *Recorder) status(ctx context.Context, state, detail string, now time.Time) {
	if err := r.cfg.Status.Set(ctx, researchstatus.ComponentQuotes, state, detail, now); err != nil {
		slog.Warn("researchquotes: 状態を保存できませんでした", "error", err)
	}
}
