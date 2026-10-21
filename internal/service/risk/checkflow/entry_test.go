package checkflow_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// Issue #840: the quantity Check settled on is the one handed to enter, with
// the price it was judged at, and a rejected candidate never reaches enter.
func TestCheckThenEnter_HandsEnterTheQuantityCheckSettledOn(t *testing.T) {
	ctx := context.Background()
	e, settings, _ := bucketEngine(t, true)

	var got []risk.EntrySize
	passed, reason, err := e.CheckThenEnter(ctx, 1, domain.JevDirectionLong, func(size risk.EntrySize) error {
		got = append(got, size)
		return nil
	})
	// testLimits: 2% of 30,000,000 = 600,000 yen per symbol, at 2,000 yen.
	if err != nil || !passed || len(got) != 1 || got[0] != (risk.EntrySize{Quantity: 300, Price: 2000}) {
		t.Fatalf("CheckThenEnter = (%v, %q, %v), enter saw %+v, want one call with 300 shares at 2000", passed, reason, err, got)
	}

	storeStockBudget(t, settings, "0")
	passed, reason, err = e.CheckThenEnter(ctx, 1, domain.JevDirectionLong, func(risk.EntrySize) error {
		t.Error("enter ran for a rejected candidate")
		return nil
	})
	if err != nil || passed || !strings.HasPrefix(reason, risk.ReasonBucketDisabled) {
		t.Fatalf("CheckThenEnter with the stock budget at 0 = (%v, %q, %v), want (false, bucket_disabled, nil)", passed, reason, err)
	}
}

// Issue #839: two candidates that together exceed the stock budget cannot
// both pass. The first holds the entry section (still opening its position)
// while the second arrives; the second must wait, then see the first's
// holdings and be rejected with bucket_budget_exceeded.
func TestCheckThenEnter_ConcurrentCandidatesCannotTogetherExceedBucketBudget(t *testing.T) {
	ctx := context.Background()
	e, settings, db := bucketEngine(t, true)
	// One 600,000-yen entry fits a 1,000,000-yen budget; two do not.
	storeStockBudget(t, settings, "1000000")

	type outcome struct {
		passed bool
		reason string
		err    error
	}
	results := make(chan outcome, 2)
	enterInto := func(size risk.EntrySize) error {
		return insertStockPosition(db, size.Quantity, size.Price, size.Price)
	}

	firstInside := make(chan struct{})
	releaseFirst := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseFirst) })
	defer release()
	go func() {
		passed, reason, err := e.CheckThenEnter(ctx, 1, domain.JevDirectionLong, func(size risk.EntrySize) error {
			close(firstInside)
			<-releaseFirst
			return enterInto(size)
		})
		results <- outcome{passed, reason, err}
	}()
	<-firstInside
	go func() {
		passed, reason, err := e.CheckThenEnter(ctx, 1, domain.JevDirectionLong, enterInto)
		results <- outcome{passed, reason, err}
	}()

	// Without the shared lock the second candidate would read zero holdings,
	// pass and finish here, while the first is still inside its entry.
	select {
	case early := <-results:
		t.Fatalf("second candidate finished while the first was still entering: %+v", early)
	case <-time.After(100 * time.Millisecond):
	}
	release()

	var passedCount, exceeded int
	for range 2 {
		select {
		case o := <-results:
			switch {
			case o.err != nil:
				t.Fatalf("CheckThenEnter error: %v", o.err)
			case o.passed:
				passedCount++
			case strings.HasPrefix(o.reason, risk.ReasonBucketBudgetExceeded):
				exceeded++
			default:
				t.Fatalf("rejected with %q, want %s", o.reason, risk.ReasonBucketBudgetExceeded)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for the candidates")
		}
	}
	if passedCount != 1 || exceeded != 1 {
		t.Fatalf("passed=%d bucket_budget_exceeded=%d, want exactly 1 of each", passedCount, exceeded)
	}
}
