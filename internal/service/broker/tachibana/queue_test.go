package tachibana_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// Higher-priority requests overtake queued lower ones.
func TestQueueServesHigherPriorityFirst(t *testing.T) {
	var g tachibana.Gate
	release, err := g.Acquire(context.Background(), tachibana.PrioritySession)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var order []tachibana.Priority
	var wg sync.WaitGroup
	start := func(p tachibana.Priority) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rel, err := g.Acquire(context.Background(), p)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			order = append(order, p)
			mu.Unlock()
			rel()
		}()
	}
	// Queue in the worst order; wait until each is really waiting.
	for i, p := range []tachibana.Priority{tachibana.PriorityMaster, tachibana.PriorityWatchQuote, tachibana.PriorityHeldQuote} {
		start(p)
		tt.Eventually(t, func() bool { return g.Waiting() == i+1 })
	}
	release()
	wg.Wait()
	want := []tachibana.Priority{tachibana.PriorityHeldQuote, tachibana.PriorityWatchQuote, tachibana.PriorityMaster}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Errorf("service order = %v, want %v", order, want)
	}
}

func TestQueueWaiterCancelledWhileQueued(t *testing.T) {
	var g tachibana.Gate
	release, _ := g.Acquire(context.Background(), tachibana.PrioritySession)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := g.Acquire(ctx, tachibana.PriorityMaster); done <- err }()
	tt.Eventually(t, func() bool { return g.Waiting() == 1 })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	release()
	rel, err := g.Acquire(context.Background(), tachibana.PriorityMaster) // the gate is free again
	if err != nil {
		t.Fatal(err)
	}
	rel()
}

// 夜間の日足取得 never leaves the queue between 8:00 and 15:30 JST.
func TestHistoryRequestsWaitOutDaytime(t *testing.T) {
	cases := []struct {
		name  string
		start time.Time
		prio  tachibana.Priority
		want  time.Time // earliest allowed send time
	}{
		{"history at 09:00 waits until 15:30", tt.AtJST(2026, 10, 8, 9, 0), tachibana.PriorityHistory, tt.AtJST(2026, 10, 8, 15, 30)},
		{"history at 08:00 waits until 15:30", tt.AtJST(2026, 10, 8, 8, 0), tachibana.PriorityHistory, tt.AtJST(2026, 10, 8, 15, 30)},
		{"history at 15:29 waits until 15:30", tt.AtJST(2026, 10, 8, 15, 29), tachibana.PriorityHistory, tt.AtJST(2026, 10, 8, 15, 30)},
		{"history at 18:00 goes straight", tt.AtJST(2026, 10, 8, 18, 0), tachibana.PriorityHistory, tt.AtJST(2026, 10, 8, 18, 0)},
		{"history at 07:59 goes straight", tt.AtJST(2026, 10, 8, 7, 59), tachibana.PriorityHistory, tt.AtJST(2026, 10, 8, 7, 59)},
		{"master at 09:00 is not restricted", tt.AtJST(2026, 10, 8, 9, 0), tachibana.PriorityMaster, tt.AtJST(2026, 10, 8, 9, 0)},
		{"watch quote at 10:00 is not restricted", tt.AtJST(2026, 10, 8, 10, 0), tachibana.PriorityWatchQuote, tt.AtJST(2026, 10, 8, 10, 0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clk := tt.NewAutoClock(tc.start)
			fb := tt.New(t, clk)
			c := newClient(t, fb, clk, 10)
			loginOK(t, c, fb)
			if err := call(c, tachibana.TargetRequest, tc.prio); err != nil {
				t.Fatal(err)
			}
			reqs := fb.Requests()
			got := reqs[len(reqs)-1].At
			if got.Before(tc.want) {
				t.Errorf("sent at %s, before the allowed %s", got.In(tachibana.JST).Format("15:04:05"), tc.want.In(tachibana.JST).Format("15:04:05"))
			}
			if tc.prio == tachibana.PriorityHistory && tachibana.InDaytime(got) {
				t.Errorf("a history request went out at %s, inside 8:00〜15:30", got.In(tachibana.JST).Format("15:04:05"))
			}
		})
	}
}
