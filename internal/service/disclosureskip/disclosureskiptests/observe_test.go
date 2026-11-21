package disclosureskiptests

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosureskip"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

var feedNow = time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC) // 10:00 JST

// scriptedFeed returns items/err per symbol and counts the requests.
type scriptedFeed struct {
	mu    sync.Mutex
	items map[string][]assist.NewsItem
	err   error
	calls int
}

func (f *scriptedFeed) Fetch(_ context.Context, symbol string) ([]assist.NewsItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.items[symbol], f.err
}

func tdnet(id, title string, at time.Time) assist.NewsItem {
	return assist.NewsItem{ID: id, Symbol: sym, Headline: title, Body: "TDnet 適時開示: https://example.test/" + id + ".pdf", PublishedAt: at.UTC()}
}

// sinkFunc is a Sink from a function.
type sinkFunc func(ctx context.Context, ev disclosureskip.FetchEvent) error

func (f sinkFunc) WriteFetch(ctx context.Context, ev disclosureskip.FetchEvent) error {
	return f(ctx, ev)
}

// collector is a Sink that keeps every event and tells the test when n arrived.
type collector struct {
	mu     sync.Mutex
	events []disclosureskip.FetchEvent
	got    chan struct{}
}

func newCollector() *collector { return &collector{got: make(chan struct{}, 64)} }

func (c *collector) WriteFetch(_ context.Context, ev disclosureskip.FetchEvent) error {
	c.mu.Lock()
	c.events = append(c.events, ev)
	c.mu.Unlock()
	c.got <- struct{}{}
	return nil
}

func waitFor(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the background writer")
	}
}

func runWriter(t *testing.T, f *disclosureskip.RecordingFeed) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

// The recorder returns the inner feed's items and error as they are, and
// describes the fetch for the writer without touching them.
func TestRecordingFeed_ReturnsTheInnerResultUnchangedAndRecordsTheFetch(t *testing.T) {
	items := []assist.NewsItem{
		tdnet("a", tanshin, jstAt(10, 13, 15, 30, 0)),
		tdnet("b", "配当予想の修正に関するお知らせ", jstAt(10, 14, 9, 0, 0)),
		{ID: "c", Symbol: sym, Headline: "no time", Body: "x"},
	}
	want := append([]assist.NewsItem(nil), items...)
	inner := &scriptedFeed{items: map[string][]assist.NewsItem{sym: items}}
	sink := newCollector()
	feed := disclosureskip.NewRecordingFeed(inner, 3, sink, disclosureskip.WithClock(func() time.Time { return feedNow }))
	runWriter(t, feed)

	got, err := feed.Fetch(context.Background(), sym)
	if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(items, want) {
		t.Fatalf("Fetch = %+v, %v; want the inner items %+v untouched", got, err, want)
	}
	waitFor(t, sink.got)
	ev := sink.events[0]
	if !ev.OK || ev.Symbol != sym || !ev.At.Equal(feedNow) || ev.ItemCount != 3 || !ev.Truncated || ev.Undated != 1 || len(ev.Items) != 2 {
		t.Fatalf("event = %+v", ev)
	}
	if !ev.Oldest.Equal(jstAt(10, 13, 15, 30, 0)) {
		t.Fatalf("Oldest = %s, want the 10-13 15:30 JST item", ev.Oldest)
	}
	if ev.Items[0].URL != "https://example.test/a.pdf" || ev.Items[0].ID != "a" || ev.Items[0].Title != tanshin {
		t.Fatalf("first item = %+v", ev.Items[0])
	}
	if inner.calls != 1 {
		t.Fatalf("the inner feed was called %d times, want 1: the recorder must add no request", inner.calls)
	}
}

func TestRecordingFeed_ReturnsTheInnerErrorUnchangedAndRecordsTheFailure(t *testing.T) {
	boom := errors.New("yanoshin down")
	inner := &scriptedFeed{err: boom}
	sink := newCollector()
	feed := disclosureskip.NewRecordingFeed(inner, 10, sink)
	runWriter(t, feed)

	items, err := feed.Fetch(context.Background(), sym)
	if !errors.Is(err, boom) || items != nil {
		t.Fatalf("Fetch = %v, %v; want nil and the inner error", items, err)
	}
	waitFor(t, sink.got)
	if ev := sink.events[0]; ev.OK || ev.ItemCount != 0 {
		t.Fatalf("event = %+v, want a failed fetch", ev)
	}
}

// A feed that made no request (backing off) or was cancelled says nothing
// about the feed, so it is returned as it is and not recorded. The queue is
// first-in first-out and one writer drains it, so when the event of a later,
// ordinary fetch is the first to arrive, the earlier ones were never queued.
func TestRecordingFeed_DoesNotRecordBackoffOrCancelledFetches(t *testing.T) {
	inner := &scriptedFeed{items: map[string][]assist.NewsItem{sym: {tdnet("a", tanshin, jstAt(10, 14, 9, 0, 0))}}}
	sink := newCollector()
	feed := disclosureskip.NewRecordingFeed(inner, 10, sink)
	runWriter(t, feed)

	inner.err = newsfeed.ErrBackingOff
	if _, err := feed.Fetch(context.Background(), sym); !errors.Is(err, newsfeed.ErrBackingOff) {
		t.Fatalf("err = %v, want ErrBackingOff", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inner.err = context.Canceled
	if _, err := feed.Fetch(ctx, sym); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	inner.err = nil
	if _, err := feed.Fetch(context.Background(), "6758"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, sink.got)
	if len(sink.events) != 1 || sink.events[0].Symbol != "6758" || !sink.events[0].OK {
		t.Fatalf("events = %+v, want only the ordinary fetch", sink.events)
	}
}

// However the recording fails - the writer errors, panics, or never runs so
// the queue fills - Fetch returns what the inner feed returned.
func TestRecordingFeed_RecordingFailuresNeverChangeFetch(t *testing.T) {
	items := []assist.NewsItem{tdnet("a", tanshin, jstAt(10, 14, 9, 0, 0))}
	boom := errors.New("yanoshin down")

	t.Run("writer returns an error", func(t *testing.T) {
		written := make(chan struct{}, 4)
		feed := disclosureskip.NewRecordingFeed(&scriptedFeed{items: map[string][]assist.NewsItem{sym: items}}, 10,
			sinkFunc(func(context.Context, disclosureskip.FetchEvent) error {
				written <- struct{}{}
				return errors.New("disk full")
			}))
		runWriter(t, feed)
		for range 2 {
			got, err := feed.Fetch(context.Background(), sym)
			if err != nil || !reflect.DeepEqual(got, items) {
				t.Fatalf("Fetch = %+v, %v", got, err)
			}
			waitFor(t, written)
		}
	})
	t.Run("writer panics and keeps running", func(t *testing.T) {
		written := make(chan struct{}, 4)
		feed := disclosureskip.NewRecordingFeed(&scriptedFeed{items: map[string][]assist.NewsItem{sym: items}}, 10,
			sinkFunc(func(context.Context, disclosureskip.FetchEvent) error { written <- struct{}{}; panic("db driver boom") }))
		runWriter(t, feed)
		for range 2 {
			got, err := feed.Fetch(context.Background(), sym)
			if err != nil || !reflect.DeepEqual(got, items) {
				t.Fatalf("Fetch = %+v, %v", got, err)
			}
			waitFor(t, written) // the second event is written: the writer survived the first panic
		}
	})
	t.Run("queue full, no writer running", func(t *testing.T) {
		inner := &scriptedFeed{items: map[string][]assist.NewsItem{sym: items}}
		feed := disclosureskip.NewRecordingFeed(inner, 10, newCollector(), disclosureskip.WithQueueSize(2))
		for range 5 {
			got, err := feed.Fetch(context.Background(), sym)
			if err != nil || !reflect.DeepEqual(got, items) {
				t.Fatalf("Fetch = %+v, %v", got, err)
			}
		}
		if feed.Dropped() != 3 {
			t.Fatalf("Dropped = %d, want 3 (5 fetches into a queue of 2)", feed.Dropped())
		}
	})
	t.Run("inner error with a full queue", func(t *testing.T) {
		feed := disclosureskip.NewRecordingFeed(&scriptedFeed{err: boom}, 10, newCollector(), disclosureskip.WithQueueSize(1))
		for range 3 {
			if _, err := feed.Fetch(context.Background(), sym); !errors.Is(err, boom) {
				t.Fatalf("err = %v, want the inner error", err)
			}
		}
	})
}
