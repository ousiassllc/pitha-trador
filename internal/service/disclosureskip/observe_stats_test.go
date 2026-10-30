package disclosureskip_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosureskip"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

// yanoshinIndex is a やのしん index of n entries published this morning
// (10-14 09:00 JST onward); the entries at the emptyTitles positions have no
// title, so the client drops them.
func yanoshinIndex(n int, emptyTitles ...int) string {
	empty := map[int]bool{}
	for _, i := range emptyTitles {
		empty[i] = true
	}
	entries := make([]string, n)
	for i := range entries {
		title := "株主総会招集ご通知"
		if empty[i] {
			title = ""
		}
		entries[i] = fmt.Sprintf(`{"Tdnet":{"id":"d%d","pubdate":"2026-10-14 09:%02d:00","title":%q,"document_url":"https://example.test/d%d.pdf"}}`, i, i, title, i)
	}
	return `{"items":[` + strings.Join(entries, ",") + `]}`
}

// feedBySymbol routes a fetch to the feed of its symbol.
type feedBySymbol map[string]*newsfeed.GuardedFeed

func (f feedBySymbol) Fetch(ctx context.Context, symbol string) ([]assist.NewsItem, error) {
	return f[symbol].Fetch(ctx, symbol)
}

func serveIndex(t *testing.T, body string) *newsfeed.GuardedFeed {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	t.Cleanup(srv.Close)
	return newsfeed.NewGuardedFeed(newsfeed.NewYanoshinClient(newsfeed.YanoshinConfig{BaseURL: srv.URL}))
}

// The title-less entries the client drops still count: a response that hit the
// item limit is cut off however many entries were left out, and a dropped
// entry makes the fetch incomplete. The recorder reaches the client through
// the guard, and the items Fetch returns are the same as without it.
func TestRecordingFeed_CountsEntriesTheFeedDroppedAndStaysUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name          string
		index         string
		wantRaw       int
		wantTruncated bool
		wantItems     int
	}{
		{"a full response with one entry dropped is still cut off", yanoshinIndex(newsfeed.YanoshinLimit, 3), newsfeed.YanoshinLimit, true, newsfeed.YanoshinLimit - 1},
		{"a short response with one entry dropped is not cut off but incomplete", yanoshinIndex(3, 1), 3, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := openDB(t)
			store := disclosureskip.NewStore(conn)
			decided := jstAt(10, 14, 10, 0, 0)
			now := decided.Add(time.Hour)
			evaluator := disclosureskip.NewEvaluator(store, weekdaysOnly, disclosureskip.WithEvaluatorClock(func() time.Time { return now }))
			insertDecision(t, conn, 1, "trader", "LONG", decided)

			inner := serveIndex(t, tc.index)
			plain, err := inner.Fetch(context.Background(), sym)
			if err != nil {
				t.Fatal(err)
			}
			sink := newCollector()
			feed := disclosureskip.NewRecordingFeed(inner, newsfeed.YanoshinLimit, sink, disclosureskip.WithClock(func() time.Time { return decided.Add(6 * time.Minute) }))
			runWriter(t, feed)
			got, err := feed.Fetch(context.Background(), sym)
			if err != nil || len(got) != tc.wantItems || !reflect.DeepEqual(got, plain) {
				t.Fatalf("Fetch = %d items, %v; want the %d items of the unrecorded fetch, unchanged", len(got), err, tc.wantItems)
			}
			waitFor(t, sink.got)
			ev := sink.events[0]
			if !ev.OK || ev.ItemCount != tc.wantRaw || ev.Truncated != tc.wantTruncated || ev.Undated != 1 || len(ev.Items) != tc.wantItems {
				t.Fatalf("event = %+v, want ItemCount %d, Truncated %v, Undated 1, %d items", ev, tc.wantRaw, tc.wantTruncated, tc.wantItems)
			}

			if err := store.WriteFetch(context.Background(), ev); err != nil {
				t.Fatal(err)
			}
			if err := evaluator.Cycle(context.Background()); err != nil {
				t.Fatal(err)
			}
			if r, ok := recordOf(t, conn, 1); !ok || r.verdict != "unknown" || r.reason != "incomplete_fetch" {
				t.Fatalf("record = %+v, %v; want unknown / incomplete_fetch", r, ok)
			}
		})
	}
}

// Fetches of different symbols run at once: each carries its own stats.
func TestRecordingFeed_ConcurrentFetchesDoNotShareStats(t *testing.T) {
	short := serveIndex(t, yanoshinIndex(2))
	full := serveIndex(t, yanoshinIndex(newsfeed.YanoshinLimit, 0))
	sink := newCollector()
	feed := disclosureskip.NewRecordingFeed(feedBySymbol{"1111": short, "2222": full}, newsfeed.YanoshinLimit, sink)
	runWriter(t, feed)

	done := make(chan struct{}, 2)
	for _, s := range []string{"1111", "2222"} {
		go func() {
			_, _ = feed.Fetch(context.Background(), s)
			done <- struct{}{}
		}()
	}
	waitFor(t, done)
	waitFor(t, done)
	waitFor(t, sink.got)
	waitFor(t, sink.got)
	for _, ev := range sink.events {
		switch ev.Symbol {
		case "1111":
			if ev.ItemCount != 2 || ev.Truncated || ev.Undated != 0 {
				t.Errorf("short event = %+v", ev)
			}
		case "2222":
			if ev.ItemCount != newsfeed.YanoshinLimit || !ev.Truncated || ev.Undated != 1 {
				t.Errorf("full event = %+v", ev)
			}
		}
	}
}
