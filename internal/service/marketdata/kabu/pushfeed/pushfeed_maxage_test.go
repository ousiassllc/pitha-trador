package pushfeed_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu/pushfeed"
)

// Issue #754: PUSH first, REST only when the PUSH board is older than the
// configured age. A board that is fresh under a wide limit is stale under a
// tight one, and the quote then comes from REST.
func TestLatest_BoardMaxAgeDecidesPushVersusREST(t *testing.T) {
	for _, tc := range []struct {
		name     string
		maxAge   time.Duration // Feed.BoardMaxAge; 0 = DefaultBoardMaxAge
		wantREST bool
	}{
		{"default age: PUSH board still fresh", 0, false},
		{"wide age: PUSH board still fresh", time.Minute, false},
		{"tight age: PUSH board aged out", time.Millisecond, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeKabu(t, marketdata.Board{Symbol: "1000", CurrentPrice: 2400},
				marketdata.Board{Symbol: "1000", CurrentPrice: 2555})
			feed := pushfeed.New(fakeUniverse{stocks(1)}, f.client, f.wsURL(), 1)
			feed.BoardMaxAge = time.Minute // generous while waiting for the PUSH board
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { feed.Run(ctx); close(done) }()
			t.Cleanup(func() { cancel(); <-done })

			deadline := time.Now().Add(5 * time.Second)
			for {
				if b, err := feed.Latest(ctx, "1000"); err == nil && b.Price == 2555 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("PUSH board never served")
				}
				time.Sleep(5 * time.Millisecond)
			}

			feed.BoardMaxAge = tc.maxAge
			time.Sleep(20 * time.Millisecond) // age the board past the tight limit
			before := f.boardFetches()
			if _, err := feed.Latest(ctx, "1000"); err != nil {
				t.Fatalf("Latest: %v", err)
			}
			if gotREST := f.boardFetches() > before; gotREST != tc.wantREST {
				t.Errorf("REST used = %v, want %v", gotREST, tc.wantREST)
			}
		})
	}
}
