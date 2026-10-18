package fillflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// settingsStore is a runtime_settings store a test edits between fills.
type settingsStore map[string]string

func (s settingsStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s[key]
	return v, ok, nil
}

type failingCosts struct{}

func (failingCosts) Costs(context.Context) (fillmodel.Costs, error) {
	return fillmodel.Costs{}, errors.New("settings unreadable")
}

func settingsEngine(t *testing.T, store settingsStore) testEngine {
	cfg := execution.DefaultConfig()
	cfg.Calendar = marketcalendar.TSE
	return newTestEngineWithCosts(t, cfg, fillmodel.SettingsSource{Store: store})
}

func enterDirection(t *testing.T, te testEngine, direction string, book fillmodel.Book) execution.EntryResult {
	t.Helper()
	return enterDirectionAt(t, te, direction, book, jstAt(29, 10, 0))
}

func enterDirectionAt(t *testing.T, te testEngine, direction string, book fillmodel.Book, now time.Time) execution.EntryResult {
	t.Helper()
	signal := longSignal(te.instrument.ID)
	signal.Direction = direction
	result, err := te.engine.Enter(context.Background(), execution.EntryRequest{
		Signal: signal, Quantity: 100, Price: 2000, Book: book, Now: now,
	})
	if err != nil {
		t.Fatalf("Enter %s: %v", direction, err)
	}
	return result
}

// Changing the stored cost settings changes the next Paper fill's price and
// fee on the SAME Engine, without rebuilding it (#779): two LONG entries of 100
// shares on one Engine, with the settings edited between them.
func TestEnter_FollowsTheCostSettings(t *testing.T) {
	ctx := context.Background()
	store := settingsStore{}
	te := settingsEngine(t, store)
	book := fillmodel.Book{Bid: 1999, Ask: 2001}

	// The defaults: ask 2001 + 2 bps = 2001.4 -> 2002, no fee.
	before := enterDirectionAt(t, te, domain.JevDirectionLong, book, jstAt(29, 10, 0))
	if got := before.Position.EntryPrice; got != 2002 {
		t.Fatalf("EntryPrice at the defaults = %v, want 2002", got)
	}
	if before.Order.Fees != 0 {
		t.Fatalf("Fees at the defaults = %v, want 0", before.Order.Fees)
	}
	if _, err := te.engine.Close(ctx, before.Position.ID, domain.ExitReasonManual, 2000, fillmodel.Book{}, jstAt(29, 10, 5)); err != nil {
		t.Fatalf("Close: %v", err)
	}

	store[costsettings.KeySlippageLongContinuous] = "30" // ask 2001 + 30 bps = 2007.003 -> 2008
	store[costsettings.KeyFeeBps] = "10"
	store[costsettings.KeyFeeFixedJPY] = "55"
	after := enterDirectionAt(t, te, domain.JevDirectionLong, book, jstAt(29, 10, 10))
	if got := after.Position.EntryPrice; got != 2008 {
		t.Errorf("EntryPrice after raising the slippage = %v, want 2008", got)
	}
	if got, want := after.Position.EntryPrice-before.Position.EntryPrice, 6.0; got != want {
		t.Errorf("entry price rose by %v, want %v (28 bps more slippage on 2001, ceiled to the tick)", got, want)
	}
	// 10 bps of the 2008 * 100 notional (200.8 JPY) + 55 JPY.
	if got, want := after.Order.Fees-before.Order.Fees, 255.8; got < want-1e-9 || got > want+1e-9 {
		t.Errorf("fee rose by %v, want %v (10 bps of the notional + 55 JPY)", got, want)
	}
}

// The slippage is set per direction: a SHORT entry (SELL) takes the SHORT
// slippage, not the LONG one.
func TestEnter_ShortUsesTheShortSlippage(t *testing.T) {
	store := settingsStore{costsettings.KeySlippageShortContinuous: "30", costsettings.KeySlippageLongContinuous: "0"}
	te := settingsEngine(t, store)
	// bid 1999 - 30 bps = 1993.0 -> 1993 (sell rounds down)
	result := enterDirection(t, te, domain.JevDirectionShort, fillmodel.Book{Bid: 1999, Ask: 2001})
	if got := result.Position.EntryPrice; got != 1993 {
		t.Fatalf("SHORT EntryPrice = %v, want 1993 (bid less the 30 bps SHORT slippage)", got)
	}
}

// A SHORT position's 貸株料 is charged with its exit and lands in the
// realized PnL; a LONG position pays none.
func TestClose_ShortBorrowFeeIsInRealizedPnL(t *testing.T) {
	ctx := context.Background()
	close := func(direction string, store settingsStore) domain.Position {
		te := settingsEngine(t, store)
		entry := enterDirection(t, te, direction, fillmodel.Book{})
		closed, err := te.engine.Close(ctx, entry.Position.ID, domain.ExitReasonManual, 2000, fillmodel.Book{}, jstAt(29, 10, 5))
		if err != nil {
			t.Fatalf("Close %s: %v", direction, err)
		}
		return closed
	}
	flat := settingsStore{costsettings.KeySlippageLongContinuous: "0", costsettings.KeySlippageShortContinuous: "0"}
	withBorrow := settingsStore{costsettings.KeySlippageLongContinuous: "0", costsettings.KeySlippageShortContinuous: "0", costsettings.KeyShortBorrowBps: "10"}

	short, shortBorrow := close(domain.JevDirectionShort, flat), close(domain.JevDirectionShort, withBorrow)
	// 10 bps of the 2000 * 100 entry notional.
	if got := *short.RealizedPnL - *shortBorrow.RealizedPnL; got < 199.999 || got > 200.001 {
		t.Errorf("SHORT PnL reduction from the 貸株料 = %v, want 200 (10 bps of 200,000)", got)
	}
	long, longBorrow := close(domain.JevDirectionLong, flat), close(domain.JevDirectionLong, withBorrow)
	if *long.RealizedPnL != *longBorrow.RealizedPnL {
		t.Errorf("LONG PnL changed with the 貸株料: %v vs %v", *long.RealizedPnL, *longBorrow.RealizedPnL)
	}
}

// An unreadable setting never blocks a fill: Config.Fill applies.
func TestFill_FallsBackToTheConfiguredCostsWhenSettingsAreUnreadable(t *testing.T) {
	cfg := execution.DefaultConfig()
	cfg.Calendar = marketcalendar.TSE
	cfg.Fill = fillmodel.Uniform(fillmodel.Model{SlippageBps: 30})
	te := newTestEngineWithCosts(t, cfg, failingCosts{})
	result := enterDirection(t, te, domain.JevDirectionLong, fillmodel.Book{Bid: 1999, Ask: 2001})
	if got := result.Position.EntryPrice; got != 2008 {
		t.Fatalf("EntryPrice = %v, want 2008 (Config.Fill's 30 bps)", got)
	}
}
