package researchquotes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/researchquote"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchstatus"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

// record stores the minute of now for every planned symbol that has a price,
// in one transaction, and reports the state. A symbol without a price gets no
// row (never a zero).
func (r *Recorder) record(ctx context.Context, now time.Time, feed Feed, symbols []string) error {
	minute := now.Truncate(time.Minute)
	samples := make([]researchquote.Sample, 0, len(symbols))
	noSession := false
	for _, symbol := range symbols {
		if err := ctx.Err(); err != nil {
			return err
		}
		q, observed, via, err := r.quoteOf(ctx, feed, symbol)
		switch {
		case err == nil:
			samples = append(samples, newSample(minute, q, observed, via))
		case errors.Is(err, broker.ErrNoSession):
			noSession = true
		}
	}
	n, err := r.cfg.Store.InsertBatch(ctx, samples)
	if err != nil {
		r.status(ctx, researchstatus.StateError, "時価を保存できませんでした", now)
		return fmt.Errorf("researchquotes: store samples: %w", err)
	}
	if len(samples) == 0 && noSession {
		r.status(ctx, researchstatus.StateError, "立花のセッションがありません（ログイン待ち）", now)
		return nil
	}
	r.status(ctx, researchstatus.StateRunning, fmt.Sprintf("記録中: 銘柄 %d、直近の分に %d 行、本日の接続 %d 回", len(symbols), n, feed.ConnectionsToday()), now)
	slog.Debug("researchquotes: minute recorded", "symbols", len(symbols), "rows", n)
	return nil
}

var errNoQuote = errors.New("researchquotes: no price")

// quoteOf is symbol's value for the minute: the feed's latest merged value
// (EVENT, or an earlier REST refill) whatever its age, with the time it was
// last updated (observed_at tells a quiet symbol from a dead connection). Only
// when there is no value at all is the feed's REST refill asked (Latest; the
// feed rate-limits it by the REST interval and answers ErrStale in between). An
// error means there is no price to record (broker.ErrNoSession when the cause
// is the missing session).
func (r *Recorder) quoteOf(ctx context.Context, feed Feed, symbol string) (broker.Quote, time.Time, string, error) {
	if q, at, ok := feed.Peek(symbol); ok {
		return q, at, viaOf(feed, symbol), nil
	}
	latest, err := feed.Latest(ctx, symbol)
	switch {
	case err == nil && latest.Price > 0:
		if q, at, ok := feed.Peek(symbol); ok {
			return q, at, viaOf(feed, symbol), nil
		}
		return latest, time.Time{}, researchquote.ViaREST, nil
	case errors.Is(err, broker.ErrNoSession):
		return broker.Quote{}, time.Time{}, "", err
	}
	return broker.Quote{}, time.Time{}, "", errNoQuote
}

func viaOf(feed Feed, symbol string) string {
	if feed.ViaREST(symbol) {
		return researchquote.ViaREST
	}
	return researchquote.ViaEvent
}
