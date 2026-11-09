package historytests

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/changeguard"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/calibration"
)

// calibrationSource is a CalibrationSource that also reports the newest meta
// model, the way internal/bootstrap/metalabeljob.CalibrationSource does.
type calibrationSource struct {
	calibration.StaticCalibrationSource
	*metalabel.Reporter
}

// calibrationMetaModel returns GET /api/v1/calibration's meta_model for the
// models stored in w.
func calibrationMetaModel(t *testing.T, r *repo.Repository) map[string]any {
	t.Helper()
	reporter := metalabel.NewReporter(r, func(context.Context) (metalabelsettings.Settings, error) {
		return metalabelsettings.Settings{LLMCutoff: "2026-09-01"}, nil
	})
	_, api := humatest.New(t)
	huma.Get(api, "/api/v1/calibration", calibration.NewCalibrationHandler(calibrationSource{Reporter: reporter}).APICalibration)
	resp := api.Get("/api/v1/calibration")
	if resp.Code != http.StatusOK {
		t.Fatalf("calibration status %d: %s", resp.Code, resp.Body.String())
	}
	var body struct {
		MetaModel map[string]any `json:"meta_model"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil || body.MetaModel == nil {
		t.Fatalf("calibration meta_model: %v, body %s", err, resp.Body.String())
	}
	return body.MetaModel
}

func fullModel() repo.Model {
	f := 4.5
	return repo.Model{
		Version: "latest", Algorithm: "logreg", LabelVersion: 2, Status: repo.ModelRejected, RejectReason: changeguard.ReasonModelSwapRateLimited,
		TrainedAt: minute(30), TrainStart: minute(1), TrainEnd: minute(10), OOSStart: minute(11), OOSEnd: minute(29),
		TrainSamples: 400, OOSSamples: 240, ParamsJSON: "{}", OOSTakes: 120, OOSDays: 9,
		OOSEVCI: &repo.Interval{Low: 1.25, High: 8.5}, OOSAUCCI: &repo.Interval{Low: 0.52, High: 0.66},
		Adoption: repo.Adoption{
			Criteria: bar(),
			Comparison: &repo.Comparison{ActiveVersion: "older", Comparable: true, Note: "NOTE-SENTINEL", PairedSamples: 240,
				CandidateBps: 3.1, ActiveBps: 1.2, DiffBps: 1.9, DiffCI: &repo.Interval{Low: 0.4, High: 3.3}, ActiveEVCI: &repo.Interval{Low: -0.5, High: 2.5}},
			Gate: &repo.Gate{Reason: changeguard.ReasonModelSwapRateLimited, Detail: "DETAIL-SENTINEL", Until: "2026-10-09T15:00:00Z"},
		},
		Forward: repo.Forward{EvaluatedAt: "2026-10-12T00:00:00Z", From: "2026-10-05T00:30:00Z", Samples: 80, Takes: 40, Days: 3,
			EVBps: &f, EVCI: &repo.Interval{Low: -2, High: 11}, Degraded: false},
	}
}

// The values of the newest model are the Calibration API's meta_model, field
// for field (comparison: minus its free-text note, plus the active model's EV
// CI the Calibration API does not show; gate: minus its detail).
func TestLatestModelMatchesTheCalibrationAPI(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	r := repo.NewRepository(w)
	if _, err := r.InsertModel(t.Context(), fullModel()); err != nil {
		t.Fatal(err)
	}
	cal := calibrationMetaModel(t, r)
	mcp := newServer(t, path).models(t)["latest"]
	if mcp == nil {
		t.Fatal("latest model missing from metalabel_history")
	}

	for _, k := range []string{"oos_takes", "oos_days", "oos_ev_ci", "oos_auc_ci", "criteria", "forward", "reject_reason", "status", "oos_samples"} {
		if !reflect.DeepEqual(mcp[k], cal[k]) {
			t.Errorf("%s: metalabel_history %v, calibration meta_model %v", k, mcp[k], cal[k])
		}
	}
	calCmp, mcpCmp := asMap(cal["comparison"]), asMap(mcp["comparison"])
	if calCmp["note"] != "NOTE-SENTINEL" {
		t.Fatalf("fixture drifted: the Calibration API's note = %v", calCmp["note"])
	}
	delete(calCmp, "note")
	if mcpCmp["active_ev_ci"] == nil {
		t.Errorf("active_ev_ci missing: %v", mcpCmp)
	}
	delete(mcpCmp, "active_ev_ci")
	if !reflect.DeepEqual(mcpCmp, calCmp) {
		t.Errorf("comparison: metalabel_history %v, calibration meta_model %v", mcpCmp, calCmp)
	}
	calGate, mcpGate := asMap(cal["gate"]), asMap(mcp["gate"])
	if mcpGate["reason"] != calGate["reason"] || mcpGate["until"] != calGate["until"] || len(mcpGate) != 2 {
		t.Errorf("gate: metalabel_history %v, calibration meta_model %v", mcpGate, calGate)
	}
	for k, want := range map[string]any{"adoption_recorded_status": "recorded", "comparison_status": "recorded", "forward_status": "evaluated"} {
		if mcp[k] != want {
			t.Errorf("%s = %v, want %v", k, mcp[k], want)
		}
	}
	// The criteria are exactly the ones the Calibration API shows, so the
	// numeric checks are judged by the same numbers.
	if got := asMap(asMap(mcp["numeric_checks"])["oos_takes"])["required"]; got != asMap(cal["criteria"])["min_takes"] {
		t.Errorf("oos_takes required = %v, want criteria min_takes %v", got, asMap(cal["criteria"])["min_takes"])
	}
}

// forward_status: evaluated once the nightly run recorded it, not_evaluated
// while forward_json is empty on a model that has an adoption record (a
// rejected or just adopted model), not_recorded for a row stored before the
// adoption record existed and for an unreadable forward_json.
func TestForwardStatus(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	fwd := repo.Forward{EvaluatedAt: "2026-10-12T00:00:00Z", From: "2026-10-05T00:00:00Z", Samples: 10, Takes: 4, Days: 2, Degraded: true}
	a := repo.Adoption{Criteria: bar()}
	insert(t, w, rowOf("evaluated", "", 1, passing(), a))
	insert(t, w, rowOf("rejected", training.RejectEV, 2, passing(), a))
	insert(t, w, rowOf("old", training.RejectOOSSamples, 3, stats{samples: 3}, "{}"))
	insert(t, w, rowOf("garbled", "", 4, passing(), a))
	testkit.MustExec(t, w, `UPDATE metalabel_models SET forward_json = ? WHERE version = 'evaluated'`, jsonText(fwd))
	testkit.MustExec(t, w, `UPDATE metalabel_models SET forward_json = 'nope' WHERE version = 'garbled'`)
	models := newServer(t, path).models(t)

	for v, want := range map[string]string{"evaluated": "evaluated", "rejected": "not_evaluated", "old": "not_recorded", "garbled": "not_recorded"} {
		if models[v]["forward_status"] != want {
			t.Errorf("%s: forward_status = %v, want %s", v, models[v]["forward_status"], want)
		}
		if (models[v]["forward"] != nil) != (want == "evaluated") {
			t.Errorf("%s: forward = %v with status %s", v, models[v]["forward"], want)
		}
	}
	got := asMap(models["evaluated"]["forward"])
	want := map[string]any{"evaluated_at": fwd.EvaluatedAt, "from": fwd.From, "samples": 10.0, "takes": 4.0, "days": 2.0, "ev_bps": nil, "ev_ci": nil, "degraded": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("forward = %v, want %v", got, want)
	}
}
