package fillmodel_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// Default is the pre-settings behaviour: no fee, 2 bps ザラ場, 5 bps 寄り/引け,
// no 貸株料, for both directions.
func TestDefaultCosts(t *testing.T) {
	d := fillmodel.Default()
	want := fillmodel.Model{SlippageBps: 2, OpeningSlippageBps: 5, ClosingSlippageBps: 5, ReferenceNotionalJPY: costsettings.DefaultReferenceNotionalJPY}
	if d.Long != want || d.Short != want {
		t.Fatalf("Default() = %+v, want %+v for both directions", d, want)
	}
	if d.Long.Fee(2000, 100) != 0 || d.Short.BorrowFee(2000, 100) != 0 || d.Short.RoundTripFeeBps() != 0 {
		t.Fatalf("Default() charges a fee: %+v", d)
	}
}

func TestFromSettingsAndFor(t *testing.T) {
	s := costsettings.Default()
	s.FeeBps = 3
	s.FeeFixedJPY = 100
	s.Long.OpeningBps = 7
	s.Short.ClosingBps = 9
	s.ShortBorrowBps = 4
	c := fillmodel.FromSettings(s)

	if long := c.For(domain.JevDirectionLong); long.OpeningSlippageBps != 7 || long.ClosingSlippageBps != 5 || long.FeeBps != 3 || long.FeeFixedJPY != 100 || long.ShortBorrowBps != 0 {
		t.Errorf("LONG model = %+v", long)
	}
	if short := c.For(domain.JevDirectionShort); short.OpeningSlippageBps != 5 || short.ClosingSlippageBps != 9 || short.ShortBorrowBps != 4 {
		t.Errorf("SHORT model = %+v", short)
	}
	if c.For("anything else") != c.Long {
		t.Error("an unknown direction is not priced as LONG")
	}
}

func TestFeesAndBorrow(t *testing.T) {
	m := fillmodel.Model{FeeBps: 10, FeeFixedJPY: 55, ReferenceNotionalJPY: 1_000_000, ShortBorrowBps: 20}
	if got, want := m.Fee(2000, 100), 200.0+55; got != want { // 10 bps of 200,000 + 55
		t.Errorf("Fee = %v, want %v", got, want)
	}
	if got, want := m.BorrowFee(2000, 100), 400.0; got != want { // 20 bps of 200,000
		t.Errorf("BorrowFee = %v, want %v", got, want)
	}
	if got, want := m.FeeBpsPerFill(), 10+0.55; got < want-1e-9 || got > want+1e-9 { // 55 JPY of 1,000,000 = 0.55 bps
		t.Errorf("FeeBpsPerFill = %v, want %v", got, want)
	}
	if got, want := m.RoundTripFeeBps(), 2*10.55+20; got < want-1e-9 || got > want+1e-9 {
		t.Errorf("RoundTripFeeBps = %v, want %v", got, want)
	}
	if got := (fillmodel.Model{FeeBps: 4, FeeFixedJPY: 55}).FeeBpsPerFill(); got != 4 { // no reference: the fixed part is not converted
		t.Errorf("FeeBpsPerFill without a reference notional = %v, want 4", got)
	}
}

// 寄り and 引け have their own slippage; the ザラ場 one applies otherwise.
func TestMarket_OpeningAndClosingSlippageAreSeparate(t *testing.T) {
	m := fillmodel.Model{SlippageBps: 0, OpeningSlippageBps: 100, ClosingSlippageBps: 200} // 1 % / 2 % of 2000 = 20 / 40 yen
	open, err := m.Market(buy, 2000, fillmodel.Book{}, marketcalendar.PhaseOpeningAuction)
	if err != nil || open != 2020 {
		t.Errorf("opening buy = %v, %v, want 2020", open, err)
	}
	closing, err := m.Market(buy, 2000, fillmodel.Book{}, marketcalendar.PhaseClosingAuction)
	if err != nil || closing != 2040 {
		t.Errorf("closing buy = %v, %v, want 2040", closing, err)
	}
	continuous, err := m.Market(buy, 2000, fillmodel.Book{}, marketcalendar.PhaseContinuous)
	if err != nil || continuous != 2000 {
		t.Errorf("continuous buy = %v, %v, want 2000", continuous, err)
	}
}

type fakeStore map[string]string

func (f fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f[key]
	return v, ok, nil
}

type errStore struct{}

func (errStore) Get(context.Context, string) (string, bool, error) {
	return "", false, errors.New("boom")
}

func TestSettingsSource_ReadsEveryCall(t *testing.T) {
	ctx := context.Background()
	store := fakeStore{}
	src := fillmodel.SettingsSource{Store: store}
	c, err := src.Costs(ctx)
	if err != nil || c != fillmodel.Default() {
		t.Fatalf("Costs = %+v, %v, want the defaults", c, err)
	}
	store[costsettings.KeyFeeBps] = "6"
	if c, err = src.Costs(ctx); err != nil || c.Long.FeeBps != 6 {
		t.Fatalf("Costs after a Settings change = %+v, %v, want fee 6", c, err)
	}
	if _, err := (fillmodel.SettingsSource{Store: errStore{}}).Costs(ctx); err == nil {
		t.Fatal("Costs hid a store failure")
	}
}
