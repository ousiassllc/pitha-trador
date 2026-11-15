package actuals

import (
	"context"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/actuals"
)

func sliceByKey(t *testing.T, name string, got []JevSlice, keys ...string) map[string]JevSlice {
	t.Helper()
	if len(got) != len(keys) {
		t.Fatalf("%s: %d slices %+v, want keys %v", name, len(got), got, keys)
	}
	m := map[string]JevSlice{}
	for i, s := range got {
		if s.Key != keys[i] {
			t.Fatalf("%s: key[%d] = %q, want %q (ordered)", name, i, s.Key, keys[i])
		}
		m[s.Key] = s
	}
	return m
}

func wantSlice(t *testing.T, name string, s JevSlice, n int, hit, brier float64) {
	t.Helper()
	if s.HitRate.N != n || s.Brier.N != n {
		t.Errorf("%s: n = %d/%d, want %d", name, s.HitRate.N, s.Brier.N, n)
	}
	near(t, name+" hit", s.HitRate.Value, hit)
	near(t, name+" brier", s.Brier.Value, brier)
}

// Jev's metrics of the synthetic database sliced by day, week, direction and
// horizon, against hand-computed values (actualstest.Outcomes): v2 grades
// decisions 1 (LONG .8 hit), 2 (SHORT .6 miss) on the 12th, 4 (LONG .9 hit)
// on the 13th and 5 (LONG .5 miss) on the 14th; v1 grades 1 (hit) on the 12th
// and 3 (LONG .7 miss) on the 13th, both at the 5-minute horizon.
func TestEvaluate_SyntheticJevSlices(t *testing.T) {
	e, err := seededService(t).Evaluate(context.Background(), Range{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(e.Jev) != 2 {
		t.Fatalf("Jev = %+v, want 2 label versions", e.Jev)
	}
	v1, v2 := e.Jev[0], e.Jev[1]

	day := sliceByKey(t, "v2 day", v2.ByDay, "2026-10-12", "2026-10-13", "2026-10-14")
	wantSlice(t, "v2 12th", day["2026-10-12"], 2, 0.5, (0.04+0.36)/2)
	wantSlice(t, "v2 13th", day["2026-10-13"], 1, 1, 0.01)
	wantSlice(t, "v2 14th", day["2026-10-14"], 1, 0, 0.25)

	week := sliceByKey(t, "v2 week", v2.ByWeek, "2026-W42")
	wantSlice(t, "v2 W42", week["2026-W42"], 4, 0.5, 0.165)

	dir := sliceByKey(t, "v2 direction", v2.ByDirection, "LONG", "SHORT")
	wantSlice(t, "v2 LONG", dir["LONG"], 3, 2.0/3, (0.04+0.01+0.25)/3)
	wantSlice(t, "v2 SHORT", dir["SHORT"], 1, 0, 0.36)

	hor := sliceByKey(t, "v2 horizon", v2.ByHorizon, "10")
	wantSlice(t, "v2 10min", hor["10"], 4, 0.5, 0.165)

	// v1 stays apart from v2 in every slice.
	d1 := sliceByKey(t, "v1 day", v1.ByDay, "2026-10-12", "2026-10-13")
	wantSlice(t, "v1 12th", d1["2026-10-12"], 1, 1, 0.04)
	wantSlice(t, "v1 13th", d1["2026-10-13"], 1, 0, 0.49)
	h1 := sliceByKey(t, "v1 horizon", v1.ByHorizon, "5")
	wantSlice(t, "v1 5min", h1["5"], 2, 0.5, 0.265)
	sliceByKey(t, "v1 direction", v1.ByDirection, "LONG")

	// A slice of one decision is far below the sample floor.
	for _, s := range append(append([]JevSlice{}, v2.ByDay...), v2.ByDirection...) {
		if s.HitRate.Reliable || s.Brier.Reliable {
			t.Errorf("slice %q with n=%d must be 判定不能", s.Key, s.HitRate.N)
		}
	}
}

// The slices of a version add up to the version's overall figure, and the
// horizon keys sort numerically ("5" < "10" < "60").
func TestJevGroups_SlicesPartitionTheVersionAndHorizonsSortNumerically(t *testing.T) {
	var samples []repo.JevSample
	base := time.Date(2026, 10, 12, 0, 30, 0, 0, time.UTC)
	for i := 0; i < 40; i++ {
		samples = append(samples, repo.JevSample{
			DecidedAt:      base.Add(time.Duration(i%8) * 24 * time.Hour), // 8 days
			Direction:      []string{"LONG", "SHORT"}[i%2],
			Confidence:     0.6,
			LabelVersion:   2,
			HorizonMinutes: []int{60, 5, 10, 5}[i%4],
			Correct:        i%3 == 0,
		})
	}
	g := jevGroups(samples, DefaultCriteria())
	if len(g) != 1 {
		t.Fatalf("groups = %d, want 1", len(g))
	}
	sliceByKey(t, "horizon", g[0].ByHorizon, "5", "10", "60")
	for name, slices := range map[string][]JevSlice{"day": g[0].ByDay, "week": g[0].ByWeek, "direction": g[0].ByDirection, "horizon": g[0].ByHorizon} {
		total := 0
		for _, s := range slices {
			total += s.HitRate.N
		}
		if total != g[0].HitRate.N || g[0].HitRate.N != 40 {
			t.Errorf("%s slices sum to %d, overall n = %d, want 40", name, total, g[0].HitRate.N)
		}
	}
	if len(g[0].ByDay) != 8 {
		t.Errorf("days = %d, want 8", len(g[0].ByDay))
	}
	// 40 samples over 8 days is enough for the overall figure to be judged.
	if !g[0].HitRate.Reliable {
		t.Errorf("overall hit rate %+v should be reliable (n=40, 8 days)", g[0].HitRate)
	}
}
