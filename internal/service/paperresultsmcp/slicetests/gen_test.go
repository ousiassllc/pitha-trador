package slicetests

import (
	"fmt"
	"testing"
	"time"
)

// October 2026 days the generated cohorts use: Mon 5 .. Fri 9.
var weekdays = []string{"2026-10-05", "2026-10-06", "2026-10-07", "2026-10-08", "2026-10-09"}

// slotTime is the JST decision time of slot k of a day: 09:00 + 20 minutes * k.
func slotTime(t *testing.T, day string, k int) time.Time {
	t.Helper()
	return jst(t, day+"T09:00:00").Add(time.Duration(k) * 20 * time.Minute)
}

// cohortSpecs generates a cohort of n decisions over the first days weekdays (n/days per day, the first n%days days one
// more), on its own instrument inst (and the sector the caller registers), with ids from idStart. The scenario cycles up,
// down, mild, dip, up and the direction alternates, so every day has a different mean. Attributes vary with the
// index so that every fixed bucket of a dimension is hit.
func cohortSpecs(t *testing.T, model string, inst, idStart, n, days int) []spec {
	t.Helper()
	scenarios := []string{"up", "down", "mild", "dip", "up"}
	regimes := []string{"trend", "mean_revert", "", "unknown"}
	var out []spec
	for i := range n {
		day, k := i%days, i/days
		dir := "LONG"
		if i%2 == 1 {
			dir = "SHORT"
		}
		out = append(out, spec{
			id: idStart + i, inst: inst, at: slotTime(t, weekdays[day], k), dir: dir, scenario: scenarios[(i+day)%len(scenarios)],
			model: model, version: v3, regime: regimes[i%len(regimes)], vol: f(float64((i * 7) % 23)), conf: f(float64((i*3)%11) / 10),
			spread: f(float64((i * 5) % 17)), turn: f(float64((i * 11) % 29)),
		})
	}
	return out
}

// lowSampleSpecs: three cohorts at the edges of the low-sample rule.
//
//	edge-ok    30 rows on 5 days  (n = MinBucketRows, days = MinBucketDays: not flagged)
//	edge-rows  29 rows on 5 days  (rows_below_min only)
//	edge-days  30 rows on 4 days  (days_below_min only)
func lowSampleSpecs(t *testing.T) (specs []spec, sectors map[int]string) {
	t.Helper()
	specs = append(specs, cohortSpecs(t, "edge-ok", 1, 100, 30, 5)...)
	specs = append(specs, cohortSpecs(t, "edge-rows", 2, 200, 29, 5)...)
	specs = append(specs, cohortSpecs(t, "edge-days", 3, 300, 30, 4)...)
	return specs, map[int]string{1: "Auto", 2: "Bank", 3: "Chem"}
}

func sectorName(i int) string { return fmt.Sprintf("sec-%02d", i) }
