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

// Issue #780: the report carries how the newest model was judged.
func TestReporter_CarriesTheAdoptionRecord(t *testing.T) {
	ev := 2.0
	m := repo.Model{
		Version: "v5", Status: repo.ModelRejected, RejectReason: "model_swap_rate_limited",
		OOSTakes: 600, OOSDays: 6, OOSEVCI: &repo.Interval{Low: 0.4, High: 5}, OOSAUCCI: &repo.Interval{Low: 0.51, High: 0.57},
		Adoption: repo.Adoption{
			Criteria:   repo.Criteria{MinOOSSamples: 1084, MinTakes: 542, MinOOSDays: 5, MinAUCLower: 0.5, Alpha: 0.01, Resamples: 2000},
			Comparison: &repo.Comparison{ActiveVersion: "v4", Comparable: true, PairedSamples: 1100, DiffBps: 1, DiffCI: &repo.Interval{Low: 0.1, High: 1.9}},
			Gate:       &repo.Gate{Reason: "model_swap_rate_limited", Detail: "2 of 5", Until: "2026-10-21T15:00:00Z"},
		},
		Forward: repo.Forward{EvaluatedAt: "2026-10-20T09:00:00Z", Samples: 300, Takes: 100, Days: 3, EVBps: &ev, EVCI: &repo.Interval{Low: -1, High: 5}},
	}
	got, _, err := metalabel.NewReporter(fakeReportRepo{latest: &m}, cutoffSettings("")).Report(context.Background())
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if got.OOSTakes != 600 || got.OOSDays != 6 || got.OOSEVCI == nil || got.OOSEVCI.Low != 0.4 || got.OOSAUCCI == nil || got.OOSAUCCI.High != 0.57 {
		t.Errorf("OOS counts/intervals = %d %d %+v %+v", got.OOSTakes, got.OOSDays, got.OOSEVCI, got.OOSAUCCI)
	}
	if got.Criteria == nil || got.Criteria.MinTakes != 542 || got.Criteria.Resamples != 2000 {
		t.Errorf("Criteria = %+v", got.Criteria)
	}
	if got.Comparison == nil || got.Comparison.ActiveVersion != "v4" || got.Comparison.DiffCI == nil || got.Comparison.DiffCI.Low != 0.1 {
		t.Errorf("Comparison = %+v", got.Comparison)
	}
	if got.Gate == nil || got.Gate.Reason != "model_swap_rate_limited" || got.Forward.Takes != 100 || got.Forward.EVCI == nil || got.Forward.EVCI.High != 5 {
		t.Errorf("Gate/Forward = %+v / %+v", got.Gate, got.Forward)
	}
	// A model stored before the adoption record existed has none.
	legacy := repo.Model{Version: "v1", Status: repo.ModelActive}
	got, _, err = metalabel.NewReporter(fakeReportRepo{latest: &legacy}, cutoffSettings("")).Report(context.Background())
	if err != nil || got.Criteria != nil || got.Comparison != nil || got.Gate != nil || got.Forward.EvaluatedAt != "" {
		t.Errorf("legacy report = %+v, %v, want no adoption record", got, err)
	}
}
