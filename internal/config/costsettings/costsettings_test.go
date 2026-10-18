package costsettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

type fakeStore struct {
	rows map[string]string
	err  error
}

func (f fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f.rows[key]
	return v, ok, f.err
}

// The shipped defaults are the cost behaviour before the settings existed:
// no fee, 2 bps ザラ場, 5 bps 寄り/引け, no 貸株料, for both directions.
func TestDefault_EqualsThePreSettingsBehaviour(t *testing.T) {
	s := costsettings.Default()
	if err := s.Validate(); err != nil {
		t.Fatalf("Default().Validate() = %v", err)
	}
	want := costsettings.Slippage{ContinuousBps: 2, OpeningBps: 5, ClosingBps: 5}
	if s.FeeBps != 0 || s.FeeFixedJPY != 0 || s.ShortBorrowBps != 0 || s.Long != want || s.Short != want {
		t.Fatalf("Default() = %+v, want fee 0 / fixed 0 / borrow 0 / slippage 2,5,5 for both directions", s)
	}
	// The label cost is unchanged at the defaults whatever its base.
	for _, direction := range []string{domain.JevDirectionLong, domain.JevDirectionShort} {
		if got := s.LabelCostBps(10, direction); got != 10 {
			t.Errorf("LabelCostBps(10, %s) at defaults = %v, want 10 (unchanged)", direction, got)
		}
	}
}

func TestApply(t *testing.T) {
	cases := []struct {
		name, key, value string
		wantErr          bool
	}{
		{"fee", costsettings.KeyFeeBps, "1.5", false},
		{"fee zero", costsettings.KeyFeeBps, "0", false},
		{"fee negative", costsettings.KeyFeeBps, "-0.1", true},
		{"fee above max", costsettings.KeyFeeBps, "1000.5", true},
		{"fixed", costsettings.KeyFeeFixedJPY, "55", false},
		{"fixed above max", costsettings.KeyFeeFixedJPY, "100001", true},
		{"short opening slippage", costsettings.KeySlippageShortOpening, "8", false},
		{"long closing slippage negative", costsettings.KeySlippageLongClosing, "-1", true},
		{"borrow", costsettings.KeyShortBorrowBps, "3", false},
		{"reference zero", costsettings.KeyReferenceNotionalJPY, "0", true},
		{"reference", costsettings.KeyReferenceNotionalJPY, "500000", false},
		{"string", costsettings.KeyFeeBps, `"1"`, true},
		{"unknown key", "cost.other", "1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := costsettings.Default()
			if err := costsettings.Apply(&s, c.key, c.value); (err != nil) != c.wantErr {
				t.Fatalf("Apply(%s, %s) error = %v, wantErr %v", c.key, c.value, err, c.wantErr)
			}
		})
	}
}

func TestKeysAreTextedAndKnown(t *testing.T) {
	s := costsettings.Default()
	for _, key := range costsettings.Keys() {
		if !costsettings.IsKey(key) {
			t.Errorf("IsKey(%s) = false", key)
		}
		if _, ok := s.Text(key); !ok {
			t.Errorf("Text(%s) not ok", key)
		}
	}
	if costsettings.IsKey("cost.other") {
		t.Error("IsKey(cost.other) = true")
	}
}

// Raising the fee or a direction's slippage raises that direction's round
// trip cost and so its label cost; SHORT additionally pays the 貸株料.
func TestLabelCost_FollowsTheModelledRoundTripCost(t *testing.T) {
	s := costsettings.Default()
	s.FeeBps = 3              // +6 bps round trip, both directions
	s.Short.ContinuousBps = 4 // +2 bps per fill vs the default, +4 round trip, SHORT only
	s.ShortBorrowBps = 2      // +2 bps, SHORT only
	s.FeeFixedJPY = 100       // 1 bp of the 1,000,000 JPY reference, per fill: +2 bps round trip
	long := s.LabelCostBps(10, domain.JevDirectionLong)
	short := s.LabelCostBps(10, domain.JevDirectionShort)
	if long != 18 {
		t.Errorf("LONG label cost = %v, want 10 + 6 + 2 = 18", long)
	}
	if short != 24 {
		t.Errorf("SHORT label cost = %v, want 10 + 6 + 2 + 4 + 2 = 24", short)
	}
	// Cheaper than the defaults never goes below zero.
	cheap := costsettings.Default()
	cheap.Long = costsettings.Slippage{}
	if got := cheap.LabelCostBps(3, domain.JevDirectionLong); got != 0 {
		t.Errorf("label cost = %v, want 0 (clamped)", got)
	}
}

func TestLoad(t *testing.T) {
	ctx := context.Background()

	got, err := costsettings.Load(ctx, fakeStore{rows: map[string]string{
		costsettings.KeyFeeBps: "2", costsettings.KeySlippageShortClosing: "9",
	}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.FeeBps != 2 || got.Short.ClosingBps != 9 || got.Long.ClosingBps != 5 {
		t.Fatalf("Load = %+v, want the stored rows over the defaults", got)
	}

	// A damaged row falls back to the defaults for every field.
	got, err = costsettings.Load(ctx, fakeStore{rows: map[string]string{
		costsettings.KeyFeeBps: "2", costsettings.KeyShortBorrowBps: "-1",
	}})
	if err != nil || got != costsettings.Default() {
		t.Fatalf("Load with an invalid row = %+v, %v; want the defaults", got, err)
	}

	boom := errors.New("boom")
	if _, err := costsettings.Load(ctx, fakeStore{err: boom}); !errors.Is(err, boom) {
		t.Fatalf("Load with a failing store error = %v, want %v", err, boom)
	}
}
