package researchdiag

import "testing"

func TestSamplePercent(t *testing.T) {
	for n, want := range map[int]int{
		0: 100, 1: 100, MedianSampleCap: 100, MedianSampleCap + 1: 99,
		2 * MedianSampleCap: 50, 10 * MedianSampleCap: 10, 1000 * MedianSampleCap: 1, 1 << 40: 1,
	} {
		if got := samplePercent(n); got != want {
			t.Errorf("samplePercent(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestMedian(t *testing.T) {
	for name, tc := range map[string]struct {
		in   []float64
		want float64
	}{"empty": {nil, 0}, "odd": {[]float64{5, 1, 3}, 3}, "even": {[]float64{4, 1, 3, 2}, 2.5}, "one": {[]float64{-7}, -7}} {
		if got := median(tc.in); got != tc.want {
			t.Errorf("%s: median = %v, want %v", name, got, tc.want)
		}
	}
}
