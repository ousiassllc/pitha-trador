package metalabeldiag

import (
	"errors"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
)

// shapes are time-ordered rows decided at random gaps (some on weekends and
// across several days), every label complete 15 minutes later: the kinds of
// history the OOS window is cut from (few rows, exactly the bar, many more,
// dense days, all before the floor, none).
func shapes(asOf time.Time) map[string][]training.Row {
	bar := training.DefaultAcceptanceConfig().MinOOSSamples
	rng := rand.New(rand.NewPCG(3, 5))
	build := func(n int, spread, newest time.Duration) []training.Row {
		rows := make([]training.Row, n)
		for i := range rows {
			at := asOf.Add(-newest - time.Duration(float64(spread)*float64(n-1-i)/float64(max(n-1, 1))))
			at = at.Add(-time.Duration(rng.IntN(int(time.Minute))))
			rows[i] = training.Row{DecidedAt: at, LabelEnd: at.Add(15 * time.Minute)}
		}
		return rows
	}
	day := 24 * time.Hour
	return map[string][]training.Row{
		"none":                     nil,
		"a few rows":               build(40, 2*day, time.Hour),
		"fewer than the bar":       build(bar-1, 30*day, time.Hour),
		"exactly the bar":          build(bar, 12*day, time.Hour),
		"more rows than the bar":   build(bar+300, 14*day, time.Hour),
		"dense recent days":        build(3*bar, 5*day, time.Hour),
		"all older than the floor": build(300, 40*day, 30*day),
		"one trading day":          build(bar+50, 3*time.Hour, 30*time.Minute),
		"newest row not yet done":  append(build(bar+50, 14*day, time.Hour), training.Row{DecidedAt: asOf.Add(-5 * time.Minute), LabelEnd: asOf.Add(10 * time.Minute)}),
	}
}

// reachOOS re-states the rule SplitWalkForward opens its window by, so it must
// open it where the split really does: on every shape and for cutoffs older
// than the longest window, inside it, at as-of and after it. A change of that
// rule breaks this test instead of leaving oos_start_reason pointing at the
// wrong cause; and the split's own result is untouched by what is reported
// (the readiness only reads it).
func TestReachOOSOpensTheWindowWhereTheSplitDoes(t *testing.T) {
	asOf := time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC)
	walk := training.DefaultWalkForwardConfig()
	cutoffs := map[string]time.Time{
		"older than the window": asOf.Add(-60 * 24 * time.Hour), "inside the window": asOf.Add(-10 * 24 * time.Hour),
		"a day ago": asOf.Add(-24 * time.Hour), "as-of": asOf, "after as-of": asOf.Add(24 * time.Hour),
	}
	for shape, rows := range shapes(asOf) {
		for cutoffName, cutoff := range cutoffs {
			t.Run(shape+" / cutoff "+cutoffName, func(t *testing.T) {
				sorted := eligibleRows(rows, asOf)
				reach := reachOOS(sorted, asOf, cutoff, walk)
				start, oos, err := describeOOS(rows, asOf, cutoff, walk)
				if err != nil {
					t.Fatalf("describeOOS: %v", err)
				}
				if !reach.start.Equal(start) {
					t.Fatalf("window opens at %v (%s), the split opens it at %v", reach.start, reach.reason, start)
				}
				if got := len(sorted) - sortedBefore(sorted, start); got != len(oos) {
					t.Errorf("rows from the start = %d, the split's OOS has %d", got, len(oos))
				}
				switch reach.reason {
				case OOSStartCutoffFloor, OOSStartMaxWindow:
					if !reach.start.Equal(reach.floor) && !reach.start.Equal(asOf) {
						t.Errorf("reason %s but the window opens at %v, not at the floor %v", reach.reason, reach.start, reach.floor)
					}
				case OOSStartMinRows, OOSStartMinDays:
					if !reach.start.After(reach.floor) {
						t.Errorf("reason %s but the window opens at its floor %v", reach.reason, reach.floor)
					}
				default:
					t.Errorf("unknown reason %q", reach.reason)
				}
				if reach.reason == OOSStartMinRows && len(oos) != walk.OOSMinRows {
					t.Errorf("min_rows window holds %d rows, want exactly %d", len(oos), walk.OOSMinRows)
				}
				if (reach.floorSource == OOSStartCutoffFloor) != (cutoff.After(asOf.Add(-walk.OOSMaxWindow))) {
					t.Errorf("floor source %s for a cutoff at %v", reach.floorSource, cutoff)
				}
			})
		}
	}
}

// sortedBefore is how many of the time-ordered rows were decided before t.
func sortedBefore(rows []training.Row, t time.Time) int {
	n := 0
	for _, r := range rows {
		if r.DecidedAt.Before(t) {
			n++
		}
	}
	return n
}

// readinessSplit reports the split SplitWalkForward makes; it adds figures
// but changes none of the split's: the skip code, the rows before the window and
// the OOS rows are those of a direct call on the same rows, so the diagnostics
// cannot move what the nightly retraining does (it never calls this code).
func TestReadinessSplitKeepsTheSplitsOwnResult(t *testing.T) {
	asOf := time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC)
	walk := training.DefaultWalkForwardConfig()
	cutoff := asOf.Add(-4 * 24 * time.Hour)
	for shape, rows := range shapes(asOf) {
		t.Run(shape, func(t *testing.T) {
			got, err := readinessSplit(rows, asOf, cutoff)
			if err != nil {
				t.Fatalf("readinessSplit: %v", err)
			}
			want, werr := training.SplitWalkForward(rows, asOf, cutoff, walk)
			if (werr == nil) != got.TrainReady {
				t.Fatalf("train ready = %v, the split said %v", got.TrainReady, werr)
			}
			if werr != nil {
				var short *training.InsufficientDataError
				if !errors.As(werr, &short) || got.TrainSkipCode == nil || *got.TrainSkipCode != short.Code || got.Train.Rows != short.Usable {
					t.Fatalf("skip = %v / %d rows, the split said %v", got.TrainSkipCode, got.Train.Rows, werr)
				}
				return
			}
			if got.OOS.Rows != len(want.OOS) || got.Train.Rows != len(want.Fit)+len(want.Calibrate) || got.OOSStart != want.OOSStart.UTC().Format(time.RFC3339) {
				t.Errorf("report = %+v, the split cut %d fit, %d calibration, %d OOS rows from %v", got, len(want.Fit), len(want.Calibrate), len(want.OOS), want.OOSStart)
			}
			if got.Shortfall.PurgedRows > want.Purged {
				t.Errorf("purged at the window boundary = %d, more than the split's %d in all", got.Shortfall.PurgedRows, want.Purged)
			}
		})
	}
}
