package execution_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

func jstAt(d, hh, mm int) time.Time {
	return time.Date(2026, time.September, d, hh, mm, 0, 0, marketcalendar.JST)
}

func calendarEngine(t *testing.T) testEngine {
	cfg := execution.DefaultConfig()
	cfg.Calendar = marketcalendar.TSE
	return newTestEngine(t, cfg)
}

func enterAt(te testEngine, at time.Time) (execution.EntryResult, error) {
	return te.engine.Enter(context.Background(), execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, Price: 2000, Now: at,
	})
}

func TestEngine_Enter_RejectsOutsideTradingSession(t *testing.T) {
	te := calendarEngine(t)
	for name, at := range map[string]time.Time{
		"night": jstAt(29, 20, 0), "lunch": jstAt(29, 12, 0), "close": jstAt(29, 15, 30),
		"saturday": jstAt(26, 10, 0), "holiday (国民の休日)": jstAt(22, 10, 0),
	} {
		if _, err := enterAt(te, at); !errors.Is(err, execution.ErrOutsideTradingSession) {
			t.Errorf("%s: Enter err = %v, want ErrOutsideTradingSession", name, err)
		}
	}
	if _, err := enterAt(te, jstAt(29, 10, 0)); err != nil {
		t.Fatalf("in-session Enter: %v", err)
	}
}

func TestEngine_OnSnapshot_ForceFlatsBeforeMarketClose(t *testing.T) {
	te := calendarEngine(t)
	ctx := context.Background()
	entry, err := enterAt(te, jstAt(29, 15, 5))
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	// force_flat_before_market_close_minutes (10): 15:19 is 11 minutes
	// before the 15:30 close - not yet; 15:20 is exactly 10 minutes.
	for _, c := range []struct {
		at   time.Time
		want bool
	}{{jstAt(29, 15, 19), false}, {jstAt(29, 15, 20), true}} {
		result, err := te.engine.OnSnapshot(ctx, snapshotAt(te.instrument.ID, 2000, c.at))
		if err != nil || result.Exited != c.want {
			t.Fatalf("OnSnapshot %s = %+v, %v; want Exited=%v", c.at.Format("15:04"), result, err, c.want)
		}
	}
	closed, err := te.positions.Get(ctx, entry.Position.ID)
	if err != nil || closed.ExitReason == nil || *closed.ExitReason != domain.ExitReasonForceFlatBeforeClose {
		t.Fatalf("position = %+v, %v; want exit reason %q", closed, err, domain.ExitReasonForceFlatBeforeClose)
	}
}
