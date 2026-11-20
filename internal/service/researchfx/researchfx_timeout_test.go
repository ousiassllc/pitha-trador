package researchfx_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/fx/gmo"
	"github.com/ousiassllc/pitha-trador/internal/service/researchfx"
)

// hangFetcher blocks until the context it is given ends, as a stuck GMO call
// (retries and backoff included) does. It records each call's deadline.
type hangFetcher struct {
	hang      bool
	deadlines []time.Time
}

func (f *hangFetcher) Klines(ctx context.Context, _ gmo.KlinesRequest) ([]gmo.Candle, error) {
	d, _ := ctx.Deadline()
	f.deadlines = append(f.deadlines, d)
	if f.hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, nil
}

func newHangRecorder(st *memStore, f *hangFetcher) *researchfx.Recorder {
	return researchfx.NewRecorder(st, func(string) (researchfx.Fetcher, error) { return f, nil },
		func(context.Context) ([]string, error) { return []string{"USD_JPY"}, nil }).
		WithClock(func() time.Time { return at("2026-10-16", 10, 0) }).WithPace(0, nil).WithLookbackDays(1)
}

// A stuck day ends at the chunk deadline instead of holding the maintenance
// runner's lock: an error, no marker, and the next Run retries the same day.
func TestRun_AStuckFetchEndsAtTheChunkDeadlineAndIsRetried(t *testing.T) {
	st, f := newStore(), &hangFetcher{hang: true}
	rec := newHangRecorder(st, f).WithChunkTimeout(time.Millisecond)
	ctx := context.Background() // alive: only the chunk deadline can end the fetch
	if _, err := rec.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run = %v, want the chunk deadline error", err)
	}
	if len(st.record) != 0 {
		t.Fatalf("a marker was written for the stuck day: %v", st.record)
	}
	f.hang = false
	if res, err := rec.Run(ctx); err != nil || res.Day != "2026-10-15" || len(st.record) != 1 {
		t.Fatalf("retry = %+v, %v, markers %v", res, err, st.record)
	}
}

// By default both requests of a Run share one deadline of ChunkTimeout.
func TestRun_BothRequestsShareTheDefaultChunkDeadline(t *testing.T) {
	st, f := newStore(), &hangFetcher{}
	start := time.Now()
	if _, err := newHangRecorder(st, f).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.deadlines) != 2 || !f.deadlines[0].Equal(f.deadlines[1]) {
		t.Fatalf("deadlines = %v, want the same one for BID and ASK", f.deadlines)
	}
	if left := f.deadlines[0].Sub(start); left < researchfx.ChunkTimeout || left > researchfx.ChunkTimeout+time.Minute {
		t.Fatalf("deadline is %v after the start, want ChunkTimeout=%v (plus the test's own delay)", left, researchfx.ChunkTimeout)
	}
}
