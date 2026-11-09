package historytests

import (
	"slices"
	"strings"
	"testing"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

func wantKeys(t *testing.T, name string, got map[string]any, keys string) {
	t.Helper()
	have := make([]string, 0, len(got))
	for k := range got {
		have = append(have, k)
	}
	slices.Sort(have)
	want := strings.Fields(keys)
	slices.Sort(want)
	if !slices.Equal(have, want) {
		t.Errorf("%s keys = %v, want %v", name, have, want)
	}
}

// Every object the adoption diagnostics add has exactly the whitelisted keys
// (docs/api/endpoints/paper-results-mcp.md §3.5), and neither the free text of
// the adoption record (comparison.note, gate.detail) nor params_json reaches
// the response.
func TestAdoptionFieldsAreWhitelistedAndCarryNoFreeText(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	if _, err := repo.NewRepository(w).InsertModel(t.Context(), fullModel()); err != nil {
		t.Fatal(err)
	}
	testkit.MustExec(t, w, `UPDATE metalabel_models SET params_json = '{"secret":"PARAMS-SENTINEL"}'`)
	raw, data := newServer(t, path).history(t, nil)

	models, _ := data["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("models = %v", data["models"])
	}
	m := models[0].(map[string]any)
	wantKeys(t, "model", m, `version algorithm label_version status reject_reason reject_reason_truncated trained_at train_start train_end
		oos_start oos_end train_samples oos_samples oos_auc oos_expected_value_bps
		oos_takes oos_days oos_ev_ci oos_auc_ci take_rate adoption_recorded_status criteria numeric_checks first_unmet numeric_complete
		comparison comparison_status gate forward forward_status`)
	wantKeys(t, "oos_ev_ci", asMap(m["oos_ev_ci"]), "low high")
	wantKeys(t, "oos_auc_ci", asMap(m["oos_auc_ci"]), "low high")
	wantKeys(t, "criteria", asMap(m["criteria"]), "min_oos_samples min_takes min_oos_days min_ev_lower_bps min_auc_lower alpha resamples")
	checks := asMap(m["numeric_checks"])
	wantKeys(t, "numeric_checks", checks, strings.Join(checkNames, " "))
	for _, name := range checkNames {
		wantKeys(t, name, asMap(checks[name]), "actual required met")
	}
	comparison := asMap(m["comparison"])
	wantKeys(t, "comparison", comparison, "active_version comparable paired_samples candidate_bps active_bps diff_bps diff_ci active_ev_ci")
	wantKeys(t, "diff_ci", asMap(comparison["diff_ci"]), "low high")
	wantKeys(t, "active_ev_ci", asMap(comparison["active_ev_ci"]), "low high")
	wantKeys(t, "gate", asMap(m["gate"]), "reason until")
	forward := asMap(m["forward"])
	wantKeys(t, "forward", forward, "evaluated_at from samples takes days ev_bps ev_ci degraded")
	wantKeys(t, "forward ev_ci", asMap(forward["ev_ci"]), "low high")

	for _, secret := range []string{"NOTE-SENTINEL", "DETAIL-SENTINEL", "PARAMS-SENTINEL", "params_json", "adoption_json", "forward_json", "seed", "note", "detail"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("the response contains %q: %s", secret, raw)
		}
	}
}

// A gate reason this build does not know is returned as the fixed code other,
// never as the stored text.
func TestUnknownGateReasonIsNormalized(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	a := repo.Adoption{Criteria: bar(), Gate: &repo.Gate{Reason: "some free text, 2 of 5 days", Until: "2026-10-09T15:00:00Z"}}
	insert(t, w, rowOf("g", "model_swap_rate_limited", 1, passing(), a))
	raw, data := newServer(t, path).history(t, nil)
	gate := asMap(asMap(data["models"].([]any)[0])["gate"])
	if gate["reason"] != "other" || strings.Contains(string(raw), "free text") {
		t.Errorf("gate = %v, want reason other and no stored text", gate)
	}
}
