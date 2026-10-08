package symbolcache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

type countingSource struct {
	calls int
	err   error
}

func (g *countingSource) SymbolInfo(context.Context, string) (broker.SymbolInfo, error) {
	g.calls++
	return broker.SymbolInfo{}, g.err
}

func TestCache_FetchesOncePerSymbolPerJSTDayAndDoesNotCacheErrors(t *testing.T) {
	g := &countingSource{err: errors.New("boom")}
	c := New(g)
	now := time.Date(2026, 10, 5, 0, 30, 0, 0, jst) // 00:30 JST
	c.now = func() time.Time { return now }
	ctx := context.Background()

	if _, err := c.Get(ctx, "7203"); err == nil {
		t.Fatal("Get: want error")
	}
	g.err = nil
	for range 3 {
		if _, err := c.Get(ctx, "7203"); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}
	if g.calls != 2 { // one failed + one successful fetch
		t.Fatalf("calls = %d, want 2 (error not cached, success cached)", g.calls)
	}

	now = now.Add(23 * time.Hour) // 23:30 JST, same trading day
	_, _ = c.Get(ctx, "7203")
	if g.calls != 2 {
		t.Errorf("same JST day refetched: calls = %d, want 2", g.calls)
	}
	now = now.Add(time.Hour) // 00:30 JST next day
	_, _ = c.Get(ctx, "7203")
	if g.calls != 3 {
		t.Errorf("next JST day not refetched: calls = %d, want 3", g.calls)
	}
}
