package disclosureskip

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

// DefaultQueueSize is how many fetch results may wait for the background
// writer. News Ingest fetches a few dozen symbols a minute, so a full queue
// means the writer is stuck: further results are dropped, never waited for.
const DefaultQueueSize = 1024

// Feed is the news feed News Ingest polls (newsfeed.Feed).
type Feed interface {
	Fetch(ctx context.Context, symbol string) ([]assist.NewsItem, error)
}

// FetchEvent is one News Ingest fetch of a symbol, copied out of the result.
type FetchEvent struct {
	Symbol string
	// At is when the request started.
	At time.Time
	OK bool
	// ItemCount is the number of items of an OK response.
	ItemCount int
	// Truncated: ItemCount reached the request's item limit.
	Truncated bool
	// Items are the response's items that have a publication time.
	Items []Disclosure
	// Undated counts the items without one.
	Undated int
	// Oldest is the oldest publication time of Items (zero when none).
	Oldest time.Time
}

// Sink persists FetchEvents (Store).
type Sink interface {
	WriteFetch(ctx context.Context, ev FetchEvent) error
}

// RecordingFeed is a Feed that behaves exactly like the one it wraps and, on
// the side, queues what each fetch returned for a background writer (Run).
// The inner result and error are returned as they are, whatever happens to the
// recording: the copy is built under a recover, queued without blocking, and a
// full or stuck queue only drops the copy. Fetches that made no request (the
// inner feed backing off) or were cut short by shutdown are not recorded: they
// say nothing about the feed.
type RecordingFeed struct {
	inner Feed
	limit int
	now   func() time.Time
	sink  Sink
	queue chan FetchEvent

	dropped atomic.Int64
}

// RecordingOption configures a RecordingFeed.
type RecordingOption func(*RecordingFeed)

// WithQueueSize overrides DefaultQueueSize.
func WithQueueSize(n int) RecordingOption {
	return func(f *RecordingFeed) {
		if n > 0 {
			f.queue = make(chan FetchEvent, n)
		}
	}
}

// WithClock overrides time.Now (tests).
func WithClock(now func() time.Time) RecordingOption {
	return func(f *RecordingFeed) { f.now = now }
}

// NewRecordingFeed wraps inner. limit is the item limit of one request
// (newsfeed.YanoshinLimit): a response of that many items may be cut off.
func NewRecordingFeed(inner Feed, limit int, sink Sink, opts ...RecordingOption) *RecordingFeed {
	f := &RecordingFeed{inner: inner, limit: limit, sink: sink, now: time.Now, queue: make(chan FetchEvent, DefaultQueueSize)}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Fetch returns inner's result unchanged.
func (f *RecordingFeed) Fetch(ctx context.Context, symbol string) ([]assist.NewsItem, error) {
	at := f.now()
	items, err := f.inner.Fetch(ctx, symbol)
	f.record(ctx, at, symbol, items, err)
	return items, err
}

// Dropped is how many fetch results were not queued because the queue was full.
func (f *RecordingFeed) Dropped() int64 { return f.dropped.Load() }

func (f *RecordingFeed) record(ctx context.Context, at time.Time, symbol string, items []assist.NewsItem, err error) {
	defer safego.Recover("disclosure skip: record fetch")
	if errors.Is(err, newsfeed.ErrBackingOff) || (err != nil && ctx.Err() != nil) {
		return
	}
	ev := FetchEvent{Symbol: symbol, At: at, OK: err == nil}
	if err == nil {
		ev = f.describe(ev, items)
	}
	select {
	case f.queue <- ev:
	default:
		if n := f.dropped.Add(1); n == 1 || n%100 == 0 {
			slog.Warn("disclosure skip: recording queue is full, fetch result dropped", "dropped_total", n)
		}
	}
}

func (f *RecordingFeed) describe(ev FetchEvent, items []assist.NewsItem) FetchEvent {
	ev.ItemCount = len(items)
	ev.Truncated = f.limit > 0 && len(items) >= f.limit
	for _, it := range items {
		if it.PublishedAt.IsZero() {
			ev.Undated++
			continue
		}
		published := it.PublishedAt.UTC()
		if ev.Oldest.IsZero() || published.Before(ev.Oldest) {
			ev.Oldest = published
		}
		id := it.ID
		if id == "" {
			id = it.Headline
		}
		ev.Items = append(ev.Items, Disclosure{
			Symbol: ev.Symbol, ID: id, PublishedAt: published, Title: it.Headline, URL: newsfeed.DocumentURL(it.Body),
		})
	}
	return ev
}

// Run writes the queued fetch results to the sink until ctx is done. A failed
// or panicking write is logged and skipped. It runs on its own goroutine,
// never on the trading path.
func (f *RecordingFeed) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-f.queue:
			if err := safego.Try("disclosure skip: write fetch", func() error { return f.sink.WriteFetch(ctx, ev) }); err != nil && ctx.Err() == nil {
				slog.Warn("disclosure skip: writing a fetch result failed, it is lost", "symbol", ev.Symbol, "error", err)
			}
		}
	}
}
