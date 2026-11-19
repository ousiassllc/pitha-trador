package disclosuresrc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/disclosuresrc"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/disclosuresrc"
)

// NewsLookback is how far back one fetch asks for news.
const NewsLookback = 24 * time.Hour

// NewsItem is one news item of the 立花 e支店 news API: the headline, the time
// and the related symbols (securities codes as the API gives them). There is no
// body: the recorder keeps headlines only.
type NewsItem struct {
	ID          string
	Headline    string
	PublishedAt time.Time
	Symbols     []string
}

// NewsFetcher fetches the news published since a time. It is the ONLY way the
// recorder reaches the 立花 news API: the production build has no implementation
// of it (the real-device measurement, issue #725, is not done), so tests pass
// a fake. An error that implements FailureCoder is recorded by its code; any
// other error is recorded as transport. The error text is never stored.
type NewsFetcher interface {
	FetchNews(ctx context.Context, since time.Time) ([]NewsItem, error)
}

// FailureCoder is implemented by a fetcher error that knows its short fixed code
// (disclosuresrc.Fail* / HTTPFailureCode).
type FailureCoder interface{ FailureCode() string }

// CredentialHolder is what the recorder asks of whoever holds the 立花
// credential: only whether it is configured. The recorder never reads the
// credential itself.
type CredentialHolder interface {
	Configured() bool
}

// TachibanaRecorder records the 立花 news headlines.
type TachibanaRecorder struct {
	store    Store
	fetcher  NewsFetcher
	creds    CredentialHolder
	settings SettingsLoader
	now      func() time.Time
}

// NewTachibanaRecorder returns a recorder of fetcher's news into store. A nil
// fetcher or creds makes every Run a no-op.
func NewTachibanaRecorder(store Store, fetcher NewsFetcher, creds CredentialHolder, settings SettingsLoader) *TachibanaRecorder {
	return &TachibanaRecorder{store: store, fetcher: fetcher, creds: creds, settings: settings, now: time.Now}
}

// WithClock replaces the clock (tests).
func (r *TachibanaRecorder) WithClock(now func() time.Time) *TachibanaRecorder { r.now = now; return r }

// Run fetches the news once. Switch off, no fetcher, or a credential that is
// not configured: it returns at once with no request and no write.
func (r *TachibanaRecorder) Run(ctx context.Context) (Result, error) {
	s, err := r.settings(ctx)
	if err != nil {
		return Result{}, err
	}
	if !s.TachibanaNewsEnabled {
		return Result{Disabled: true}, nil
	}
	if r.fetcher == nil || r.creds == nil || !r.creds.Configured() {
		return Result{NotConfigured: true}, nil
	}
	now := r.now()
	ok, err := r.store.BeginRequest(ctx, disclosuresrc.SourceTachibanaNews, repo.Day(now), s.TachibanaNewsDailyCap, now)
	if err != nil {
		return Result{}, fmt.Errorf("disclosuresrc: %w", err)
	}
	if !ok {
		return Result{CapReached: true}, nil
	}
	res := Result{Requests: 1}
	items, err := r.fetcher.FetchNews(ctx, now.Add(-NewsLookback))
	if err != nil {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		res.Failures = 1
		code := newsFailureCode(err)
		slog.WarnContext(ctx, "disclosuresrc: tachibana news request failed", "code", code)
		if ferr := r.store.RecordFailure(ctx, disclosuresrc.SourceTachibanaNews, code, r.now()); ferr != nil {
			return res, fmt.Errorf("disclosuresrc: %w", ferr)
		}
		return res, nil
	}
	n, err := r.store.UpsertTachibanaNews(ctx, rows(items), r.now())
	if err != nil {
		return res, fmt.Errorf("disclosuresrc: %w", err)
	}
	res.Rows = n
	if err := r.store.RecordSuccess(ctx, disclosuresrc.SourceTachibanaNews, r.now()); err != nil {
		return res, fmt.Errorf("disclosuresrc: %w", err)
	}
	if res.Purged, err = r.store.Purge(ctx, now.AddDate(0, 0, -s.RetentionDays)); err != nil {
		return res, fmt.Errorf("disclosuresrc: tachibana news retention: %w", err)
	}
	return res, nil
}

// rows turns news items into table rows: one per distinct related symbol
// (normalized to the disclosure_items format), or one with symbol "" when the
// item has none. An item without an id or a time is dropped.
func rows(items []NewsItem) []disclosuresrc.TachibanaNews {
	var out []disclosuresrc.TachibanaNews
	for _, it := range items {
		if it.ID == "" || it.PublishedAt.IsZero() {
			continue
		}
		seen := map[string]bool{}
		for _, code := range it.Symbols {
			sym, ok := disclosuresrc.NormalizeSymbol(code)
			if !ok || seen[sym] {
				continue
			}
			seen[sym] = true
			out = append(out, disclosuresrc.TachibanaNews{NewsID: it.ID, Symbol: sym, Headline: it.Headline, PublishedAt: it.PublishedAt})
		}
		if len(seen) == 0 {
			out = append(out, disclosuresrc.TachibanaNews{NewsID: it.ID, Headline: it.Headline, PublishedAt: it.PublishedAt})
		}
	}
	return out
}

func newsFailureCode(err error) string {
	var fc FailureCoder
	if errors.As(err, &fc) {
		return fc.FailureCode()
	}
	return disclosuresrc.FailTransport
}
