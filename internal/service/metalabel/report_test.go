package metalabel_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

type fakeReportRepo struct {
	latest, active       *repo.Model
	latestErr, activeErr error
}

func (f fakeReportRepo) LatestModel(context.Context) (repo.Model, bool, error) {
	if f.latest == nil {
		return repo.Model{}, false, f.latestErr
	}
	return *f.latest, true, f.latestErr
}

func (f fakeReportRepo) LatestActiveModel(context.Context) (repo.Model, bool, error) {
	if f.active == nil {
		return repo.Model{}, false, f.activeErr
	}
	return *f.active, true, f.activeErr
}

func cutoffSettings(string) func(context.Context) (metalabelsettings.Settings, error) {
	return func(context.Context) (metalabelsettings.Settings, error) {
		s := metalabelsettings.Default()
		s.LLMCutoff = "2026-09-01"
		return s, nil
	}
}

func TestReporter_NoModelIsNotFound(t *testing.T) {
	_, found, err := metalabel.NewReporter(fakeReportRepo{}, cutoffSettings("")).Report(context.Background())
	if err != nil || found {
		t.Fatalf("Report = found %v, err %v, want not found", found, err)
	}
}

func TestReporter_ReportsNewestModelWithOOSMetricsAndEvaluationPeriod(t *testing.T) {
	auc, ev := 0.61, 3.5
	trained := time.Date(2026, 10, 14, 18, 0, 0, 0, time.UTC)
	m := repo.Model{
		Version: "v2", Algorithm: "logistic", Status: repo.ModelActive, TrainedAt: trained,
		TrainStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), TrainEnd: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		OOSStart: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), OOSEnd: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
		TrainSamples: 300, OOSSamples: 120, OOSAUC: &auc, OOSExpectedValueBps: &ev,
		OOSCalibration: []repo.CalibrationBin{{MeanPredicted: 0.55, ObservedRate: 0.5, Count: 40}},
	}
	got, found, err := metalabel.NewReporter(fakeReportRepo{latest: &m, active: &m}, cutoffSettings("")).Report(context.Background())
	if err != nil || !found {
		t.Fatalf("Report = found %v, err %v", found, err)
	}
	if got.Version != "v2" || got.ActiveVersion != "v2" || got.Status != repo.ModelActive {
		t.Errorf("version/active/status = %q/%q/%q, want v2/v2/active", got.Version, got.ActiveVersion, got.Status)
	}
	if !got.OOSStart.Equal(m.OOSStart) || !got.OOSEnd.Equal(m.OOSEnd) || got.OOSSamples != 120 || got.TrainSamples != 300 {
		t.Errorf("windows/samples not carried: %+v", got)
	}
	if got.OOSAUC == nil || *got.OOSAUC != auc || got.OOSEVBps == nil || *got.OOSEVBps != ev {
		t.Errorf("OOS metrics = %v / %v, want %v / %v", got.OOSAUC, got.OOSEVBps, auc, ev)
	}
	if len(got.OOSCalibration) != 1 || got.OOSCalibration[0].Count != 40 || got.OOSCalibration[0].ObservedRate != 0.5 {
		t.Errorf("calibration = %+v", got.OOSCalibration)
	}
	if got.LLMCutoff != "2026-09-01" {
		t.Errorf("LLMCutoff = %q, want 2026-09-01", got.LLMCutoff)
	}
}

func TestReporter_RejectedNewestModelKeepsOlderActiveVersion(t *testing.T) {
	rejected := repo.Model{Version: "v3", Status: repo.ModelRejected, RejectReason: "oos auc below bar"}
	active := repo.Model{Version: "v2", Status: repo.ModelActive}
	got, found, err := metalabel.NewReporter(fakeReportRepo{latest: &rejected, active: &active}, cutoffSettings("")).Report(context.Background())
	if err != nil || !found {
		t.Fatalf("Report = found %v, err %v", found, err)
	}
	if got.Version != "v3" || got.Status != repo.ModelRejected || got.RejectReason != "oos auc below bar" || got.ActiveVersion != "v2" {
		t.Errorf("report = %+v, want rejected v3 with active v2", got)
	}
	// nil AUC/EV (undefined) stay nil.
	if got.OOSAUC != nil || got.OOSEVBps != nil {
		t.Errorf("undefined metrics = %v / %v, want nil", got.OOSAUC, got.OOSEVBps)
	}
}

func TestReporter_NoActiveModelLeavesActiveVersionEmpty(t *testing.T) {
	rejected := repo.Model{Version: "v1", Status: repo.ModelRejected}
	got, _, err := metalabel.NewReporter(fakeReportRepo{latest: &rejected}, cutoffSettings("")).Report(context.Background())
	if err != nil || got.ActiveVersion != "" {
		t.Fatalf("ActiveVersion = %q, err %v, want empty", got.ActiveVersion, err)
	}
}

func TestReporter_PropagatesErrors(t *testing.T) {
	boom := errors.New("boom")
	m := repo.Model{Version: "v1"}
	if _, _, err := metalabel.NewReporter(fakeReportRepo{latestErr: boom}, cutoffSettings("")).Report(context.Background()); !errors.Is(err, boom) {
		t.Errorf("latest error = %v, want boom", err)
	}
	if _, _, err := metalabel.NewReporter(fakeReportRepo{latest: &m, activeErr: boom}, cutoffSettings("")).Report(context.Background()); !errors.Is(err, boom) {
		t.Errorf("active error = %v, want boom", err)
	}
	failing := func(context.Context) (metalabelsettings.Settings, error) { return metalabelsettings.Settings{}, boom }
	if _, _, err := metalabel.NewReporter(fakeReportRepo{latest: &m}, failing).Report(context.Background()); !errors.Is(err, boom) {
		t.Errorf("settings error = %v, want boom", err)
	}
}

func TestReporter_CutoffReportsStatusAndProvenanceWithoutAnyModel(t *testing.T) {
	loaded := func(mut func(*metalabelsettings.Settings)) *metalabel.Reporter {
		return metalabel.NewReporter(fakeReportRepo{}, func(context.Context) (metalabelsettings.Settings, error) {
			s := metalabelsettings.Default()
			mut(&s)
			return s, nil
		})
	}

	placeholder, err := loaded(func(*metalabelsettings.Settings) {}).Cutoff(context.Background())
	if err != nil || placeholder.Status != metalabelsettings.CutoffPlaceholder || placeholder.Warning == "" || placeholder.Cutoff != metalabelsettings.DefaultLLMCutoff {
		t.Fatalf("placeholder = %+v, %v", placeholder, err)
	}
	confirmed, err := loaded(func(s *metalabelsettings.Settings) {
		s.LLMCutoff, s.LLMCutoffModel, s.LLMCutoffConfirmedOn, s.LLMCutoffSource = "2026-09-01", "jev-x", "2026-10-17", "vendor docs"
	}).Cutoff(context.Background())
	if err != nil || confirmed.Status != metalabelsettings.CutoffConfirmed || confirmed.Warning != "" ||
		confirmed.Model != "jev-x" || confirmed.ConfirmedOn != "2026-10-17" || confirmed.Source != "vendor docs" {
		t.Fatalf("confirmed = %+v, %v", confirmed, err)
	}

	boom := errors.New("settings down")
	if _, err := metalabel.NewReporter(fakeReportRepo{}, func(context.Context) (metalabelsettings.Settings, error) {
		return metalabelsettings.Settings{}, boom
	}).Cutoff(context.Background()); !errors.Is(err, boom) {
		t.Errorf("settings failure = %v, want it wrapped", err)
	}
}
