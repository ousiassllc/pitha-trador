package trainingflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
)

func TestTrainer_TrainsEvaluatesOutOfSampleAndStoresTheModelVersionAndMetrics(t *testing.T) {
	store := newStore(t)
	src := &fakeSource{rows: trainingRows(3000, 1)}
	logs := captureLogs(t)
	tr := newTrainer(src, store, settingsOf(nil))

	saved, err := tr.Train(context.Background())
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if saved.ID == 0 || saved.Version != "lr-20261014-090000" || saved.Algorithm != metalabel.AlgorithmLogisticRegression ||
		saved.LabelVersion != domain.CalibrationLabelVersionTripleBarrier {
		t.Errorf("saved = %+v", saved)
	}
	if saved.Status != repo.ModelActive || saved.RejectReason != "" {
		t.Fatalf("status = %q (%s), want active", saved.Status, saved.RejectReason)
	}
	if saved.OOSAUC == nil || *saved.OOSAUC < 0.6 || saved.OOSExpectedValueBps == nil || *saved.OOSExpectedValueBps <= 0 {
		t.Errorf("OOS AUC/EV = %v/%v, want a learned signal (AUC > 0.6, EV > 0)", saved.OOSAUC, saved.OOSExpectedValueBps)
	}
	if saved.OOSSamples < 400 || len(saved.OOSCalibration) == 0 {
		t.Errorf("OOS samples %d, curve %v", saved.OOSSamples, saved.OOSCalibration)
	}
	// Leak prevention: the model saw nothing past TrainEnd, and the OOS window
	// starts at least an embargo after it.
	if saved.TrainEnd.Add(training.DefaultWalkForwardConfig().Embargo).After(saved.OOSStart) {
		t.Errorf("train end %v + embargo overlaps the OOS start %v", saved.TrainEnd, saved.OOSStart)
	}
	if !saved.OOSEnd.Equal(trainNow) || !saved.OOSStart.Equal(trainNow.Add(-7*24*time.Hour)) {
		t.Errorf("OOS window = [%v, %v), want the last 7 days", saved.OOSStart, saved.OOSEnd)
	}
	if !src.to.Equal(trainNow) || !src.from.Equal(trainNow.Add(-training.DefaultLookback)) {
		t.Errorf("rows read from %v to %v", src.from, src.to)
	}

	got, found, err := store.LatestActiveModel(context.Background())
	if err != nil || !found || got.ID != saved.ID {
		t.Fatalf("LatestActiveModel = %+v, %v, %v", got, found, err)
	}
	model, err := metalabel.ParseModel(got)
	if err != nil {
		t.Fatalf("the stored model cannot be loaded for inference: %v", err)
	}
	if model.Version != saved.Version {
		t.Errorf("parsed version = %q", model.Version)
	}

	// The evaluation window and the LLM cutoff it respects are in the log.
	out := logs.String()
	for _, want := range []string{"metalabel: walk-forward windows", "llm_cutoff=2026-09-01", "oos_start=2026-10-07", "oos_end=2026-10-14", "oos_after_llm_cutoff=true", "purged="} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
}

func TestTrainer_JevFeatureEvaluationIsLimitedToAfterTheCutoff(t *testing.T) {
	store := newStore(t)
	cutoff := trainNow.Add(-36 * time.Hour)
	logs := captureLogs(t)
	tr := newTrainer(&fakeSource{rows: trainingRows(3000, 2)}, store, settingsOf(func(s *metalabelsettings.Settings) {
		s.LLMCutoff = cutoff.Format("2006-01-02")
	}))
	saved, err := tr.Train(context.Background())
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	day := time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, time.UTC)
	if !saved.OOSStart.Equal(day) {
		t.Fatalf("OOS window starts %v, want the cutoff day %v (later than now-7d)", saved.OOSStart, day)
	}
	// The cutoff day (57h of 20-minute rows, about 170) rather than the default
	// 7 days (about 500).
	if saved.OOSSamples == 0 || saved.OOSSamples > 180 {
		t.Errorf("OOS samples = %d, want only decisions after the cutoff", saved.OOSSamples)
	}
	if !strings.Contains(logs.String(), "llm_cutoff="+day.Format("2006-01-02")) || !strings.Contains(logs.String(), "oos_after_llm_cutoff=true") {
		t.Errorf("log lacks the cutoff/evaluation period:\n%s", logs.String())
	}
}

func TestTrainer_CutoffLeavingTooLittleOOSRejectsTheModelAndKeepsTheOldOne(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	old, err := newTrainer(&fakeSource{rows: trainingRows(3000, 3)}, store, settingsOf(nil)).Train(ctx)
	if err != nil || old.Status != repo.ModelActive {
		t.Fatalf("first Train = %+v, %v", old, err)
	}

	// The LLM's cutoff is tomorrow: nothing is evaluable, so the new model is
	// not adopted.
	later := newTrainer(&fakeSource{rows: trainingRows(3000, 4)}, store, settingsOf(func(s *metalabelsettings.Settings) { s.LLMCutoff = "2026-10-15" }),
		training.WithClock(func() time.Time { return trainNow.Add(time.Hour) }))
	rejected, err := later.Train(ctx)
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if rejected.Status != repo.ModelRejected || rejected.RejectReason != training.RejectOOSSamples || rejected.OOSSamples != 0 {
		t.Fatalf("model = %+v, want rejected for oos samples", rejected)
	}
	got, found, err := store.LatestActiveModel(ctx)
	if err != nil || !found || got.ID != old.ID {
		t.Fatalf("LatestActiveModel = %+v, %v, %v, want the earlier active model %d", got, found, err, old.ID)
	}
}

func TestTrainer_FailureKeepsTheExistingModel(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	old, err := newTrainer(&fakeSource{rows: trainingRows(3000, 5)}, store, settingsOf(nil)).Train(ctx)
	if err != nil {
		t.Fatalf("first Train: %v", err)
	}
	boom := errors.New("db down")

	for name, tr := range map[string]*training.Trainer{
		"source fails":   newTrainer(&fakeSource{err: boom}, store, settingsOf(nil)),
		"save fails":     newTrainer(&fakeSource{rows: trainingRows(3000, 6)}, failingSaver{err: boom}, settingsOf(nil)),
		"settings fail":  newTrainer(&fakeSource{rows: trainingRows(3000, 6)}, store, func(context.Context) (metalabelsettings.Settings, error) { return metalabelsettings.Settings{}, boom }),
		"bad cutoff":     newTrainer(&fakeSource{rows: trainingRows(3000, 6)}, store, settingsOf(func(s *metalabelsettings.Settings) { s.LLMCutoff = "soon" })),
		"unknown model":  newTrainer(&fakeSource{rows: trainingRows(3000, 6)}, store, settingsOf(func(s *metalabelsettings.Settings) { s.Model = "lightgbm" })),
		"bad calibrator": newTrainer(&fakeSource{rows: trainingRows(3000, 6)}, store, settingsOf(func(s *metalabelsettings.Settings) { s.Calibration = "beta" })),
	} {
		later := trainNow.Add(time.Hour)
		_ = later
		if err := tr.Retrain(ctx); err == nil {
			t.Errorf("%s: Retrain succeeded, want an error", name)
		}
		got, found, err := store.LatestActiveModel(ctx)
		if err != nil || !found || got.ID != old.ID {
			t.Errorf("%s: LatestActiveModel = %+v, %v, %v, want the existing model %d", name, got, found, err, old.ID)
		}
	}
	if err := newTrainer(&fakeSource{err: boom}, store, settingsOf(nil)).Retrain(ctx); !errors.Is(err, boom) {
		t.Errorf("Retrain error = %v, want it to wrap the source error", err)
	}
}

func TestTrainer_TooLittleOrOneSidedHistoryIsNotAFailureAndStoresNothing(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	logs := captureLogs(t)

	oneSided := trainingRows(3000, 7)
	for i := range oneSided {
		oneSided[i].NetReturnBps = 25 // every decision wins: nothing to separate
	}
	for name, rows := range map[string][]repo.TrainingRow{
		"no history":  nil,
		"few rows":    trainingRows(40, 8),
		"one-sided":   oneSided,
		"all dropped": {{DecidedAt: trainNow.Add(-48 * time.Hour), Direction: "LONG", StateJSON: `not json`, ResponseJSON: `{}`, HorizonMinutes: 15}},
	} {
		tr := newTrainer(&fakeSource{rows: rows}, store, settingsOf(nil))
		if _, err := tr.Train(ctx); !errors.Is(err, training.ErrInsufficientData) {
			t.Errorf("%s: Train error = %v, want ErrInsufficientData", name, err)
		}
		if err := tr.Retrain(ctx); err != nil {
			t.Errorf("%s: Retrain error = %v, want nil (a normal early-days condition)", name, err)
		}
	}
	if _, found, err := store.LatestActiveModel(ctx); err != nil || found {
		t.Errorf("LatestActiveModel found = %v, err %v, want no model stored", found, err)
	}
	if !strings.Contains(logs.String(), "nightly retraining skipped") {
		t.Errorf("skipped retraining was not logged:\n%s", logs.String())
	}
}
