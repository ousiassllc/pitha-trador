package metamodel_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain/metamodel"
)

func TestEVVerdict(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	excludesZeroAbove := &metamodel.Interval{Low: 2, High: 30}
	excludesZeroBelow := &metamodel.Interval{Low: -30, High: -2}
	straddlesZero := &metamodel.Interval{Low: -4, High: 30}

	tests := map[string]struct {
		takes int
		ev    *float64
		ci    *metamodel.Interval
		want  string
	}{
		"no takes is no data, whatever the EV":    {0, f(50), excludesZeroAbove, metamodel.EVNoData},
		"no takes and nothing else":               {0, nil, nil, metamodel.EVNoData},
		"takes but no interval":                   {600, f(50), nil, metamodel.EVUndetermined},
		"takes but no EV":                         {600, nil, excludesZeroAbove, metamodel.EVUndetermined},
		"edge just below 11 bps is undetermined":  {600, f(10.99), excludesZeroAbove, metamodel.EVUndetermined},
		"edge of exactly 11 bps is a verdict":     {600, f(11), excludesZeroAbove, metamodel.EVPositive},
		"loss just below 11 bps is undetermined":  {600, f(-10.99), excludesZeroBelow, metamodel.EVUndetermined},
		"loss of exactly 11 bps is a verdict":     {600, f(-11), excludesZeroBelow, metamodel.EVNegative},
		"large edge but the interval straddles 0": {600, f(25), straddlesZero, metamodel.EVUndetermined},
		"large loss but the interval straddles 0": {600, f(-25), &metamodel.Interval{Low: -30, High: 4}, metamodel.EVUndetermined},
		"zero EV":                                   {600, f(0), straddlesZero, metamodel.EVUndetermined},
		"large edge with the interval above 0":      {600, f(25), excludesZeroAbove, metamodel.EVPositive},
		"large EV whose interval sits on the other": {600, f(25), excludesZeroBelow, metamodel.EVUndetermined},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := metamodel.EVVerdict(tc.takes, tc.ev, tc.ci); got != tc.want {
				t.Errorf("EVVerdict(%d, %v, %v) = %q, want %q", tc.takes, tc.ev, tc.ci, got, tc.want)
			}
		})
	}
}
