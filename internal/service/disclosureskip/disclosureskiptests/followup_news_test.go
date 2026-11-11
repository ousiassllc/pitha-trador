package disclosureskiptests

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosureskip"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

// The Followup only fetches and drops the response: News Ingest's cache, news
// context and news flag are exactly what they are without it, and the polled
// symbol's are unchanged, so Jev's input and the event-driven trigger do not
// move.
func TestFollowup_DoesNotTouchNewsIngest(t *testing.T) {
	const dropped = "9999"
	decided := jstAt(10, 14, 10, 0, 0)
	items := []assist.NewsItem{tdnet("a", tanshin, decided.Add(-time.Hour))}
	newService := func(feed newsfeed.Feed) *newsfeed.Service {
		return newsfeed.NewService(feed, lunaStub{}, oneSymbol{}, newsfeed.WithNow(func() time.Time { return decided }))
	}
	plain := newService(&scriptedFeed{items: map[string][]assist.NewsItem{sym: items}})

	conn := openDB(t)
	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, name, market, is_active) VALUES (2, ?, 'y', 'TSE Prime', 1)`, dropped); err != nil {
		t.Fatal(err)
	}
	store := disclosureskip.NewStore(conn)
	inner := &swappableFeed{items: map[string][]assist.NewsItem{
		sym:     items,
		dropped: {{ID: "z", Symbol: dropped, Headline: tanshin, Body: "x", PublishedAt: decided.Add(-time.Minute).UTC()}},
	}}
	written := make(chan struct{}, 8)
	recording := disclosureskip.NewRecordingFeed(inner, newsfeed.YanoshinLimit, sinkFunc(func(ctx context.Context, ev disclosureskip.FetchEvent) error {
		err := store.WriteFetch(ctx, ev)
		written <- struct{}{}
		return err
	}), disclosureskip.WithClock(func() time.Time { return decided.Add(8 * time.Minute) }))
	runWriter(t, recording)
	withFollowup := newService(recording)

	if _, err := conn.Exec(`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
		question_version, response_json, direction, confidence, latency_ms, model_id)
		VALUES (1, 2, ?, ?, 'trader', 'h', '{}', 'trader-v3', '{}', 'LONG', 0.7, 5, 'm')`, dropped, sqlutil.FormatTime(decided)); err != nil {
		t.Fatal(err)
	}
	followup := disclosureskip.NewFollowup(store, recording, weekdaysOnly, disclosureskip.WithFollowupClock(func() time.Time { return decided.Add(8 * time.Minute) }))

	for range 2 {
		if err := plain.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := withFollowup.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
		waitFor(t, written)
		if err := followup.Cycle(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, written)

	if inner.calls[dropped] == 0 {
		t.Fatal("the followup made no request for the dropped symbol; the test does not exercise it")
	}
	wantCtx, wantOK := plain.NewsContext(sym)
	gotCtx, gotOK := withFollowup.NewsContext(sym)
	if gotOK != wantOK || !reflect.DeepEqual(gotCtx, wantCtx) {
		t.Fatalf("NewsContext(%s) = %+v, %v; without the followup %+v, %v", sym, gotCtx, gotOK, wantCtx, wantOK)
	}
	if _, ok := withFollowup.NewsContext(dropped); ok {
		t.Fatal("the dropped symbol's response entered News Ingest's cache (news_context)")
	}
	if withFollowup.TakeNewsFlag(dropped) {
		t.Fatal("the followup raised a news flag for the dropped symbol")
	}
	if got, want := withFollowup.TakeNewsFlag(sym), plain.TakeNewsFlag(sym); got != want {
		t.Fatalf("TakeNewsFlag(%s) = %v, without the followup %v", sym, got, want)
	}
	if got, want := withFollowup.TDnetScore(dropped, decided), 0.0; got != want {
		t.Fatalf("TDnetScore(%s) = %v, want %v", dropped, got, want)
	}
}
