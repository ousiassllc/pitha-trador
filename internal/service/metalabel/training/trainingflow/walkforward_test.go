package trainingflow_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
)

var wfNow = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// wfRows returns n rows, one every step, ending right before end; each label
// window is horizon long.
func wfRows(n int, step, horizon time.Duration, end time.Time) []training.Row {
	rows := make([]training.Row, n)
	for i := range rows {
		at := end.Add(-time.Duration(n-i) * step)
		rows[i] = training.Row{JevDecisionID: int64(i + 1), DecidedAt: at, LabelEnd: at.Add(horizon), Direction: "LONG", Vector: []float64{1}}
	}
	return rows
}

func wfConfig() training.WalkForwardConfig {
	return training.WalkForwardConfig{OOSWindow: 24 * time.Hour, CalibrationFraction: 0.25, Embargo: 30 * time.Minute, MinFit: 10, MinCalibration: 5}
}

func TestSplitWalkForward_OrdersSegmentsAndPurgesLabelOverlap(t *testing.T) {
	cfg := wfConfig()
	horizon := 15 * time.Minute
	// A row every 10 minutes for ~10 days: far more than the minimums.
	rows := wfRows(1440, 10*time.Minute, horizon, wfNow.Add(-time.Minute))
	s, err := training.SplitWalkForward(rows, wfNow, time.Time{}, cfg)
	if err != nil {
		t.Fatalf("SplitWalkForward: %v", err)
	}
	if want := wfNow.Add(-24 * time.Hour); !s.OOSStart.Equal(want) || !s.OOSEnd.Equal(wfNow) {
		t.Fatalf("OOS window = [%v, %v), want [%v, %v)", s.OOSStart, s.OOSEnd, want, wfNow)
	}
	if len(s.Fit) == 0 || len(s.Calibrate) == 0 || len(s.OOS) == 0 {
		t.Fatalf("segments fit/calibrate/oos = %d/%d/%d, want all non-empty", len(s.Fit), len(s.Calibrate), len(s.OOS))
	}
	calStart := s.Calibrate[0].DecidedAt
	for _, r := range s.Fit {
		if !r.DecidedAt.Before(calStart) || r.LabelEnd.Add(cfg.Embargo).After(calStart) {
			t.Fatalf("fit row at %v (label end %v) leaks into the calibration segment starting %v", r.DecidedAt, r.LabelEnd, calStart)
		}
	}
	for _, r := range s.Calibrate {
		if !r.DecidedAt.Before(s.OOSStart) || r.LabelEnd.Add(cfg.Embargo).After(s.OOSStart) {
			t.Fatalf("calibration row at %v (label end %v) leaks into the OOS window starting %v", r.DecidedAt, r.LabelEnd, s.OOSStart)
		}
	}
	for _, r := range s.OOS {
		if r.DecidedAt.Before(s.OOSStart) {
			t.Fatalf("OOS row at %v is before the OOS start %v", r.DecidedAt, s.OOSStart)
		}
	}
	if s.TrainEnd.Add(cfg.Embargo).After(s.OOSStart) {
		t.Errorf("TrainEnd %v + embargo is after OOSStart %v", s.TrainEnd, s.OOSStart)
	}
	if !s.TrainStart.Equal(s.Fit[0].DecidedAt) {
		t.Errorf("TrainStart = %v, want the first fit row %v", s.TrainStart, s.Fit[0].DecidedAt)
	}
	// Horizon 15m + embargo 30m: 45 minutes before each boundary = rows at 10-minute
	// spacing, 4 or 5 per boundary are purged (two boundaries).
	if s.Purged < 6 || s.Purged > 12 {
		t.Errorf("Purged = %d, want the label-overlapping rows before the two boundaries (6..12)", s.Purged)
	}
}

func TestSplitWalkForward_OOSNeverStartsBeforeTheLLMCutoff(t *testing.T) {
	cfg := wfConfig()
	rows := wfRows(1440, 10*time.Minute, 15*time.Minute, wfNow.Add(-time.Minute))

	cutoff := wfNow.Add(-6 * time.Hour) // later than now-OOSWindow
	s, err := training.SplitWalkForward(rows, wfNow, cutoff, cfg)
	if err != nil {
		t.Fatalf("SplitWalkForward: %v", err)
	}
	if !s.OOSStart.Equal(cutoff) || !s.LLMCutoff.Equal(cutoff) {
		t.Fatalf("OOSStart = %v (cutoff %v), want %v", s.OOSStart, s.LLMCutoff, cutoff)
	}
	for _, r := range s.OOS {
		if r.DecidedAt.Before(cutoff) {
			t.Fatalf("OOS holds a decision from %v, before the cutoff %v", r.DecidedAt, cutoff)
		}
	}
	// What is before the cutoff may train, never evaluate.
	if len(s.Calibrate) == 0 || !s.Calibrate[len(s.Calibrate)-1].DecidedAt.Before(cutoff) {
		t.Error("calibration rows must all be before the OOS start")
	}

	// A cutoff earlier than now-OOSWindow does not widen the window.
	s, err = training.SplitWalkForward(rows, wfNow, wfNow.AddDate(0, -1, 0), cfg)
	if err != nil || !s.OOSStart.Equal(wfNow.Add(-24*time.Hour)) {
		t.Fatalf("early cutoff: OOSStart = %v, err %v, want now-24h", s.OOSStart, err)
	}

	// A cutoff in the future leaves no OOS window at all.
	s, err = training.SplitWalkForward(rows, wfNow, wfNow.Add(24*time.Hour), cfg)
	if err != nil {
		t.Fatalf("future cutoff: %v", err)
	}
	if len(s.OOS) != 0 || !s.OOSStart.Equal(wfNow) || !s.OOSEnd.Equal(wfNow) {
		t.Errorf("future cutoff: OOS = %d rows in [%v, %v), want none", len(s.OOS), s.OOSStart, s.OOSEnd)
	}
}

func TestSplitWalkForward_IgnoresRowsWhoseLabelIsNotKnownYet(t *testing.T) {
	cfg := wfConfig()
	rows := wfRows(1440, 10*time.Minute, 15*time.Minute, wfNow.Add(-time.Minute))
	// Two rows decided just now: their label windows end after now.
	rows = append(rows,
		training.Row{DecidedAt: wfNow.Add(-5 * time.Minute), LabelEnd: wfNow.Add(10 * time.Minute), Direction: "LONG", Vector: []float64{1}},
		training.Row{DecidedAt: wfNow.Add(time.Minute), LabelEnd: wfNow.Add(16 * time.Minute), Direction: "LONG", Vector: []float64{1}},
	)
	s, err := training.SplitWalkForward(rows, wfNow, time.Time{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range s.OOS {
		if r.LabelEnd.After(wfNow) {
			t.Fatalf("OOS holds a row whose label ends %v, after now %v", r.LabelEnd, wfNow)
		}
	}
}

func TestSplitWalkForward_InsufficientData(t *testing.T) {
	cfg := wfConfig()
	// Only 12 rows before the OOS window: fewer than MinFit+MinCalibration.
	rows := wfRows(12, time.Hour, 15*time.Minute, wfNow.Add(-48*time.Hour))
	if _, err := training.SplitWalkForward(rows, wfNow, time.Time{}, cfg); !errors.Is(err, training.ErrInsufficientData) {
		t.Fatalf("error = %v, want ErrInsufficientData", err)
	}
	if _, err := training.SplitWalkForward(nil, wfNow, time.Time{}, cfg); !errors.Is(err, training.ErrInsufficientData) {
		t.Fatalf("no rows: error = %v, want ErrInsufficientData", err)
	}
}

func TestSplitWalkForward_PurgeCanStarveTheFitSet(t *testing.T) {
	cfg := wfConfig()
	cfg.Embargo = 400 * time.Hour // longer than all the history: everything is purged
	rows := wfRows(300, time.Hour, 15*time.Minute, wfNow.Add(-48*time.Hour))
	if _, err := training.SplitWalkForward(rows, wfNow, time.Time{}, cfg); !errors.Is(err, training.ErrInsufficientData) {
		t.Fatalf("error = %v, want ErrInsufficientData", err)
	}
}

func TestWalkForwardConfig_Validate(t *testing.T) {
	if err := training.DefaultWalkForwardConfig().Validate(); err != nil {
		t.Fatalf("default: %v", err)
	}
	for name, mut := range map[string]func(*training.WalkForwardConfig){
		"zero OOS window":  func(c *training.WalkForwardConfig) { c.OOSWindow = 0 },
		"fraction 0":       func(c *training.WalkForwardConfig) { c.CalibrationFraction = 0 },
		"fraction 1":       func(c *training.WalkForwardConfig) { c.CalibrationFraction = 1 },
		"negative embargo": func(c *training.WalkForwardConfig) { c.Embargo = -1 },
		"zero min fit":     func(c *training.WalkForwardConfig) { c.MinFit = 0 },
		"zero min cal":     func(c *training.WalkForwardConfig) { c.MinCalibration = 0 },
	} {
		cfg := training.DefaultWalkForwardConfig()
		mut(&cfg)
		if cfg.Validate() == nil {
			t.Errorf("%s: Validate accepted %+v", name, cfg)
		}
		if _, err := training.SplitWalkForward(nil, wfNow, time.Time{}, cfg); err == nil || errors.Is(err, training.ErrInsufficientData) {
			t.Errorf("%s: SplitWalkForward error = %v, want a config error", name, err)
		}
	}
}
