package mcptests

import (
	"net/http"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

type calBucket struct {
	Low         float64  `json:"low"`
	High        float64  `json:"high"`
	SampleCount int      `json:"sample_count"`
	HitRate     *float64 `json:"hit_rate"`
}

type calVersion struct {
	LabelVersion     int         `json:"label_version"`
	SampleCount      int         `json:"sample_count"`
	ExcludedCount    int         `json:"excluded_count"`
	HitRate          *float64    `json:"hit_rate"`
	Brier            *float64    `json:"brier"`
	Buckets          []calBucket `json:"buckets"`
	OutOfBucketCount int         `json:"out_of_bucket_count"`
	BarrierCounts    struct {
		TP      int `json:"tp"`
		SL      int `json:"sl"`
		Timeout int `json:"timeout"`
	} `json:"barrier_counts"`
	AvgNetReturnBps *float64 `json:"avg_net_return_bps"`
}

type calData struct {
	Decisions struct {
		Scout  int `json:"scout_count"`
		Trader int `json:"trader_count"`
	} `json:"decisions"`
	Horizons []struct {
		HorizonMinutes int          `json:"horizon_minutes"`
		ByLabelVersion []calVersion `json:"by_label_version"`
	} `json:"horizons"`
}

func checkBuckets(t *testing.T, got []calBucket, wantN [5]int, wantHit [5]*float64) {
	t.Helper()
	bands := [5][2]float64{{0.5, 0.6}, {0.6, 0.7}, {0.7, 0.8}, {0.8, 0.9}, {0.9, 1.0}}
	if len(got) != 5 {
		t.Fatalf("want 5 buckets, got %+v", got)
	}
	for i, b := range got {
		if b.Low != bands[i][0] || b.High != bands[i][1] || b.SampleCount != wantN[i] {
			t.Errorf("bucket %d = %+v, want band %v n=%d", i, b, bands[i], wantN[i])
		}
		switch {
		case wantHit[i] == nil && b.HitRate != nil:
			t.Errorf("bucket %d hit_rate = %v, want null", i, *b.HitRate)
		case wantHit[i] != nil:
			near(t, "bucket hit_rate", b.HitRate, *wantHit[i])
		}
	}
}

// Hand computation (seedJev), period JST 2026-10-01..10-10:
//
//	decisions: 1 scout, 5 trader (id 7 is outside the period)
//	horizon 5 / label 2: ids 1 (conf .55, correct, tp, +12), 2 (.65, wrong,
//	  sl, -8), 3 (.95, NULL, timeout, 0): sample 3, excluded 1,
//	  hit 1/2, brier ((.55-1)^2 + (.65-0)^2)/2 = 0.3125, avg net 4/3.
//	horizon 10 / label 1: ids 1 (.55 ok), 2 (.65 wrong), 3 (.95 ok),
//	  4 (conf NULL, ok), 6 (.30 wrong): sample 5, excluded 1 (conf NULL),
//	  hit 3/5, brier (.2025 + .4225 + .0025 + .09)/4 = 0.179375,
//	  buckets .5-.6 1/1, .6-.7 0/1, .9-1.0 1/1, out_of_bucket 1 (the .30).
func TestJevCalibration(t *testing.T) {
	srv := seededServer(t, nil)
	var d calData
	srv.call(t, "jev_calibration", period).data(t, "jev_calibration", &d)
	if d.Decisions.Scout != 1 || d.Decisions.Trader != 5 {
		t.Fatalf("decisions = %+v", d.Decisions)
	}
	if len(d.Horizons) != 2 || d.Horizons[0].HorizonMinutes != 5 || d.Horizons[1].HorizonMinutes != 10 {
		t.Fatalf("horizons = %+v", d.Horizons)
	}
	h5 := d.Horizons[0].ByLabelVersion
	if len(h5) != 1 {
		t.Fatalf("h5 versions = %+v", h5)
	}
	v2 := h5[0]
	if v2.LabelVersion != 2 || v2.SampleCount != 3 || v2.ExcludedCount != 1 || v2.OutOfBucketCount != 0 ||
		v2.BarrierCounts.TP != 1 || v2.BarrierCounts.SL != 1 || v2.BarrierCounts.Timeout != 1 {
		t.Fatalf("h5 label 2 = %+v", v2)
	}
	near(t, "h5 hit_rate", v2.HitRate, 0.5)
	near(t, "h5 brier", v2.Brier, 0.3125)
	near(t, "h5 avg_net_return_bps", v2.AvgNetReturnBps, 4.0/3.0)
	checkBuckets(t, v2.Buckets, [5]int{1, 1, 0, 0, 0}, [5]*float64{new(1.0), new(0.0), nil, nil, nil})

	h10 := d.Horizons[1].ByLabelVersion
	if len(h10) != 1 {
		t.Fatalf("h10 versions = %+v", h10)
	}
	v1 := h10[0]
	if v1.LabelVersion != 1 || v1.SampleCount != 5 || v1.ExcludedCount != 1 || v1.OutOfBucketCount != 1 ||
		v1.BarrierCounts.TP+v1.BarrierCounts.SL+v1.BarrierCounts.Timeout != 0 {
		t.Fatalf("h10 label 1 = %+v", v1)
	}
	near(t, "h10 hit_rate", v1.HitRate, 0.6)
	near(t, "h10 brier", v1.Brier, 0.179375)
	wantNull(t, "h10 avg_net_return_bps", v1.AvgNetReturnBps)
	checkBuckets(t, v1.Buckets, [5]int{1, 1, 0, 0, 1}, [5]*float64{new(1.0), new(0.0), nil, nil, new(1.0)})

	var one calData
	srv.call(t, "jev_calibration", args(map[string]any{"horizon_minutes": 5})).data(t, "jev_calibration", &one)
	if len(one.Horizons) != 1 || one.Horizons[0].HorizonMinutes != 5 {
		t.Fatalf("horizon filter = %+v", one.Horizons)
	}
}

// More than 8 horizons is limit_exceeded with no partial result.
func TestJevCalibrationHorizonLimit(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedPositions(t, w)
	testkit.Decision(t, w, 1, "trader", 0.55, "2026-10-01T01:00:00Z")
	for h := 1; h <= 9; h++ {
		testkit.Outcome(t, w, 1, h, 1, 1, nil, nil)
	}
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path})
	srv.call(t, "jev_calibration", period).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)

	// 8 horizons still fit.
	if _, err := w.Exec(`DELETE FROM calibration_outcomes WHERE horizon_minutes = 9`); err != nil {
		t.Fatal(err)
	}
	var d calData
	srv.call(t, "jev_calibration", period).data(t, "jev_calibration", &d)
	if len(d.Horizons) != 8 {
		t.Fatalf("horizons = %d, want 8", len(d.Horizons))
	}
}
