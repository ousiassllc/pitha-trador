package wsflow_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

func timePtr(t time.Time) *time.Time { return &t }

// mutableSymbolProvider lets a test publish a new snapshot while the
// handler is polling.
type mutableSymbolProvider struct {
	*fakeSymbolProvider
	mu sync.Mutex
	// polled receives one signal per State call (dropped when full), so a test
	// can wait for poll steps instead of sleeping.
	polled chan struct{}
}

func (p *mutableSymbolProvider) State(ctx context.Context, symbol string) (execution.SymbolState, error) {
	select {
	case p.polled <- struct{}{}:
	default:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fakeSymbolProvider.State(ctx, symbol)
}

func (p *mutableSymbolProvider) publish(price float64, at time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state.LastPrice = price
	p.state.LastScanAt = &at
}

// Issue #557: a tick is pushed only when a new snapshot appears, and it
// carries the snapshot time, so a stalled scanner draws no fake bars.
func TestSymbolHandler_WebSocket_TickOnlyOnNewSnapshot(t *testing.T) {
	first := time.Date(2026, 10, 5, 9, 30, 15, 0, time.UTC)
	provider := &mutableSymbolProvider{fakeSymbolProvider: &fakeSymbolProvider{
		state: execution.SymbolState{Symbol: "7203", LastPrice: 100, LastScanAt: &first},
	}, polled: make(chan struct{}, 1024)}
	ctx, conn := dialSymbolWS(t, provider)

	type tick struct {
		Type  string    `json:"type"`
		Price float64   `json:"price"`
		Time  time.Time `json:"time"`
	}
	read := func(readCtx context.Context) (tick, error) {
		var got tick
		_, data, err := conn.Read(readCtx)
		if err != nil {
			return got, err
		}
		return got, json.Unmarshal(data, &got)
	}

	got, err := read(ctx)
	if err != nil || got.Price != 100 || !got.Time.Equal(first) {
		t.Fatalf("first tick = %+v, err = %v, want price 100 at %v", got, err, first)
	}

	// Several poll steps run with the same snapshot; any repeated tick they
	// pushed would be queued ahead of the next one read below. Draining first
	// leaves only steps that started after the first tick was read.
	for drained := false; !drained; {
		select {
		case <-provider.polled:
		default:
			drained = true
		}
	}
	const pollSteps = 6
	for range pollSteps {
		select {
		case <-provider.polled:
		case <-ctx.Done():
			t.Fatalf("poll step did not run again before the deadline: %v", ctx.Err())
		}
	}

	second := first.Add(time.Minute)
	provider.publish(101, second)
	got, err = read(ctx)
	if err != nil || got.Price != 101 || !got.Time.Equal(second) {
		t.Fatalf("second tick = %+v, err = %v, want price 101 at %v", got, err, second)
	}
}
