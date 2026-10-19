package metamodel_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain/metamodel"
)

func TestEVVerdict(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	iv := func(lo, hi float64) *metamodel.Interval { return &metamodel.Interval{Low: lo, High: hi} }

	tests := map[string]struct {
		takes int
		ev    *float64
		ci    *metamodel.Interval
		want  string
	}{
		"no takes is no data, whatever the rest":        {0, f(50), iv(2, 30), metamodel.EVNoData},
		"no takes and nothing else":                     {0, nil, nil, metamodel.EVNoData},
		"interval above 0 is positive even below 11":    {600, f(8), iv(1, 15), metamodel.EVPositive},
		"interval below 0 is negative even below 11":    {600, f(-8), iv(-15, -1), metamodel.EVNegative},
		"large edge with the interval above 0":          {600, f(25), iv(3, 47), metamodel.EVPositive},
		"lower limit exactly 0 does not exclude 0":      {600, f(10.9), iv(0, 20), metamodel.EVUndetermined},
		"upper limit exactly 0 does not exclude 0":      {600, f(-10.9), iv(-20, 0), metamodel.EVUndetermined},
		"straddling 0 at 10.9 bps is undetermined":      {600, f(10.9), iv(-3, 25), metamodel.EVUndetermined},
		"straddling 0 at 11.0 bps is not significant":   {600, f(11), iv(-3, 25), metamodel.EVNotSignificant},
		"straddling 0 at -10.9 bps is undetermined":     {600, f(-10.9), iv(-25, 3), metamodel.EVUndetermined},
		"straddling 0 at -11.0 bps is not significant":  {600, f(-11), iv(-25, 3), metamodel.EVNotSignificant},
		"no interval and a small EV is undetermined":    {600, f(4), nil, metamodel.EVUndetermined},
		"no interval and a large EV is not significant": {600, f(40), nil, metamodel.EVNotSignificant},
		"no EV and no interval is undetermined":         {600, nil, nil, metamodel.EVUndetermined},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := metamodel.EVVerdict(tc.takes, tc.ev, tc.ci); got != tc.want {
				t.Errorf("EVVerdict(%d, %v, %v) = %q, want %q", tc.takes, tc.ev, tc.ci, got, tc.want)
			}
		})
	}
}
