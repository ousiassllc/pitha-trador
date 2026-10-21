package jevprompt

import "testing"

func TestUSDDropsFloatNoiseAndTrailingZeros(t *testing.T) {
	for in, want := range map[float64]string{
		0.0054600000000000004: "0.00546",
		0.1:                   "0.1",
		0.00084:               "0.00084",
		0:                     "0",
		2:                     "2",
		0.30000000000000004:   "0.3",
	} {
		if got := usd(in); got != want {
			t.Errorf("usd(%v) = %q, want %q", in, got, want)
		}
	}
}
