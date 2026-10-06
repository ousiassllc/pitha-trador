package newsfeed_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

var t0 = time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC)

type fakeFeed struct {
	items map[string][]assist.NewsItem
	err   error
}

func (f *fakeFeed) Fetch(_ context.Context, symbol string) ([]assist.NewsItem, error) {
	return f.items[symbol], f.err
}

type fakeLuna struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeLuna) Classify(_ context.Context, item assist.NewsItem) (assist.Classification, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return assist.Classification{}, f.err
	}
	return assist.Classification{Sentiment: domain.NewsSentimentBullish, EventType: domain.NewsEventEarnings, Summary: "S:" + item.Headline}, nil
}

type fakeSymbols []string

func (f fakeSymbols) NewsSymbols(context.Context) ([]string, error) { return f, nil }

func article(id, headline string, age time.Duration) assist.NewsItem {
	return assist.NewsItem{ID: id, Symbol: "7203", Headline: headline, PublishedAt: t0.Add(-age)}
}

func newService(feed *fakeFeed, luna *fakeLuna, opts ...newsfeed.Option) *newsfeed.Service {
	return newsfeed.NewService(feed, luna, fakeSymbols{"7203"}, append([]newsfeed.Option{newsfeed.WithNow(func() time.Time { return t0 })}, opts...)...)
}

func TestPoll_ClassifiesNewsIntoCacheNewestFirstAndRaisesFlag(t *testing.T) {
	feed := &fakeFeed{items: map[string][]assist.NewsItem{"7203": {article("a", "old", time.Hour), article("b", "new", time.Minute)}}}
	svc := newService(feed, &fakeLuna{})

	if err := svc.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	ctx, ok := svc.NewsContext("7203")
	if !ok || len(ctx.Items) != 2 || ctx.Items[0].Summary != "S:new" || ctx.Items[1].Summary != "S:old" {
		t.Fatalf("NewsContext = %+v, %v, want 2 items newest first", ctx, ok)
	}
	if !svc.TakeNewsFlag("7203") {
		t.Error("TakeNewsFlag = false after new news, want true")
	}
	if svc.TakeNewsFlag("7203") {
		t.Error("TakeNewsFlag = true on second call, want the flag consumed")
	}
}

func TestPoll_AlreadyClassifiedArticlesAreNotSentToLunaAgain(t *testing.T) {
	feed := &fakeFeed{items: map[string][]assist.NewsItem{"7203": {article("a", "x", time.Minute)}}}
	luna := &fakeLuna{}
	svc := newService(feed, luna)

	_ = svc.Poll(context.Background())
	svc.TakeNewsFlag("7203")
	_ = svc.Poll(context.Background())

	if luna.calls != 1 {
		t.Errorf("Luna calls = %d, want 1 (dedupe by article id)", luna.calls)
	}
	if svc.TakeNewsFlag("7203") {
		t.Error("re-polling the same article raised the news flag again")
	}
}

func TestPoll_LunaFailureRaisesNoFlagAndCachesNothing(t *testing.T) {
	feed := &fakeFeed{items: map[string][]assist.NewsItem{"7203": {article("a", "x", time.Minute), article("b", "y", time.Minute)}}}
	luna := &fakeLuna{err: errors.New("luna down")}
	svc := newService(feed, luna)

	if err := svc.Poll(context.Background()); err != nil {
		t.Fatalf("Poll returned %v, want nil: Luna failures must not surface (FR-LUNA-4)", err)
	}
	if svc.TakeNewsFlag("7203") {
		t.Error("news flag raised despite Luna failure")
	}
	if _, ok := svc.NewsContext("7203"); ok {
		t.Error("news context cached despite Luna failure")
	}
	if luna.calls != 1 {
		t.Errorf("Luna calls = %d, want 1: stop hammering Luna for the rest of the symbol's articles", luna.calls)
	}

	// Luna recovers: the unclassified articles are retried next cycle.
	luna.err = nil
	_ = svc.Poll(context.Background())
	if ctx, ok := svc.NewsContext("7203"); !ok || len(ctx.Items) != 2 {
		t.Errorf("after recovery NewsContext = %+v, %v, want both articles", ctx, ok)
	}
}

func TestPoll_FeedFailureRaisesNoFlag(t *testing.T) {
	svc := newService(&fakeFeed{err: errors.New("feed down")}, &fakeLuna{})
	if err := svc.Poll(context.Background()); err != nil {
		t.Fatalf("Poll returned %v, want nil (FR-LUNA-4)", err)
	}
	if svc.TakeNewsFlag("7203") {
		t.Error("news flag raised despite feed failure")
	}
}

func TestPoll_ExpiredArticlesAreIgnoredAndCacheEntriesExpire(t *testing.T) {
	now := t0
	feed := &fakeFeed{items: map[string][]assist.NewsItem{"7203": {article("stale", "stale", 3*time.Hour), article("fresh", "fresh", time.Minute)}}}
	luna := &fakeLuna{}
	svc := newsfeed.NewService(feed, luna, fakeSymbols{"7203"},
		newsfeed.WithNow(func() time.Time { return now }), newsfeed.WithTTL(time.Hour))

	_ = svc.Poll(context.Background())
	if luna.calls != 1 {
		t.Errorf("Luna calls = %d, want 1: an article older than the TTL is never classified", luna.calls)
	}
	if ctx, ok := svc.NewsContext("7203"); !ok || len(ctx.Items) != 1 || ctx.Items[0].Summary != "S:fresh" {
		t.Fatalf("NewsContext = %+v, %v, want only the fresh article", ctx, ok)
	}

	now = t0.Add(2 * time.Hour)
	if _, ok := svc.NewsContext("7203"); ok {
		t.Error("NewsContext still present after TTL")
	}
	if svc.TakeNewsFlag("7203") {
		t.Error("news flag still raised after every cached item expired")
	}
}

func TestPoll_KeepsOnlyMostRecentNItems(t *testing.T) {
	feed := &fakeFeed{items: map[string][]assist.NewsItem{"7203": {
		article("1", "one", 4*time.Minute), article("2", "two", 3*time.Minute), article("3", "three", 2*time.Minute), article("4", "four", time.Minute),
	}}}
	svc := newService(feed, &fakeLuna{}, newsfeed.WithMaxItems(2))
	_ = svc.Poll(context.Background())

	ctx, _ := svc.NewsContext("7203")
	if len(ctx.Items) != 2 || ctx.Items[0].Summary != "S:four" || ctx.Items[1].Summary != "S:three" {
		t.Errorf("NewsContext = %+v, want the 2 most recent items", ctx)
	}
}

func TestPoll_ArticleWithoutPublishedAtIsClassifiedOnceAndDedupedByHeadline(t *testing.T) {
	undated := assist.NewsItem{Symbol: "7203", Headline: "日付なし速報"}
	feed := &fakeFeed{items: map[string][]assist.NewsItem{"7203": {undated}}}
	luna := &fakeLuna{}
	svc := newService(feed, luna)

	_ = svc.Poll(context.Background())
	_ = svc.Poll(context.Background())

	if luna.calls != 1 {
		t.Errorf("Luna calls = %d, want 1 (an undated article is new once, then deduped)", luna.calls)
	}
	if ctx, ok := svc.NewsContext("7203"); !ok || len(ctx.Items) != 1 {
		t.Errorf("NewsContext = %+v, %v, want the undated article cached", ctx, ok)
	}
}

// countingFeed records which symbols were fetched and the peak number of
// Fetch calls in flight.
type countingFeed struct {
	mu      sync.Mutex
	fetched []string
	active  int
	peak    int
	release chan struct{}
}

func (f *countingFeed) Fetch(ctx context.Context, symbol string) ([]assist.NewsItem, error) {
	f.mu.Lock()
	f.fetched = append(f.fetched, symbol)
	f.active++
	f.peak = max(f.peak, f.active)
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
		}
	}
	return nil, nil
}

func TestPoll_FetchesOnlyTheSourceSymbols(t *testing.T) {
	feed := &countingFeed{}
	svc := newsfeed.NewService(feed, &fakeLuna{}, fakeSymbols{"7203", "9984"}, newsfeed.WithNow(func() time.Time { return t0 }))
	if err := svc.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	slices.Sort(feed.fetched)
	if want := []string{"7203", "9984"}; !slices.Equal(feed.fetched, want) {
		t.Errorf("fetched = %v, want exactly %v", feed.fetched, want)
	}
}

func TestPoll_SkipsOutsideSession(t *testing.T) {
	feed := &countingFeed{}
	open := false
	svc := newsfeed.NewService(feed, &fakeLuna{}, fakeSymbols{"7203"},
		newsfeed.WithNow(func() time.Time { return t0 }),
		newsfeed.WithSessionGate(func(time.Time) bool { return open }))

	if err := svc.Poll(context.Background()); err != nil {
		t.Fatalf("Poll (closed): %v", err)
	}
	if len(feed.fetched) != 0 {
		t.Fatalf("fetched %v outside the session, want no feed requests", feed.fetched)
	}

	open = true
	if err := svc.Poll(context.Background()); err != nil {
		t.Fatalf("Poll (open): %v", err)
	}
	if len(feed.fetched) != 1 {
		t.Errorf("fetched %v inside the session, want one request", feed.fetched)
	}
}

// The fetches overlap (a slow feed does not serialize the cycle) but never
// exceed the configured concurrency.
func TestPoll_BoundedConcurrency(t *testing.T) {
	const limit = 3
	symbols := make(fakeSymbols, 12)
	for i := range symbols {
		symbols[i] = fmt.Sprintf("%04d", 1000+i)
	}
	feed := &countingFeed{release: make(chan struct{})}
	svc := newsfeed.NewService(feed, &fakeLuna{}, symbols, newsfeed.WithConcurrency(limit))

	done := make(chan error, 1)
	go func() { done <- svc.Poll(context.Background()) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		feed.mu.Lock()
		active := feed.active
		feed.mu.Unlock()
		if active == limit {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d fetches in flight, want %d overlapping", active, limit)
		}
		time.Sleep(time.Millisecond)
	}
	close(feed.release)
	if err := <-done; err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if feed.peak != limit {
		t.Errorf("peak concurrent fetches = %d, want %d", feed.peak, limit)
	}
	if len(feed.fetched) != len(symbols) {
		t.Errorf("fetched %d symbols, want %d", len(feed.fetched), len(symbols))
	}
}
