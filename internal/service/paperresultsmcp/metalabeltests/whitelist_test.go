package metalabeltests

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Every object of metalabel_training_readiness and jev_models has exactly the
// whitelisted keys of the API spec (docs/api/endpoints/paper-results-mcp-
// metalabel.md): a column that is not in these sets cannot be in a response.
// The other tools' keys are pinned in mcptests.
func TestResponseKeysAreExactlyTheWhitelist(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	history(t, w, now, 10, 5)
	insertSkip(t, w, now.Add(-10*time.Hour), 10)
	srv := newTestServer(t, path, now)

	data := func(tool string) map[string]any {
		var d map[string]any
		srv.call(t, tool, fullLookback).data(t, tool, &d)
		return d
	}
	obj := func(m map[string]any, key string) map[string]any { return m[key].(map[string]any) }
	first := func(d map[string]any, key string) map[string]any {
		list, ok := d[key].([]any)
		if !ok || len(list) == 0 {
			t.Fatalf("%q is not a non-empty list in %v", key, d)
		}
		return list[0].(map[string]any)
	}
	want := func(name string, got map[string]any, wantKeys string) {
		t.Helper()
		keys := make([]string, 0, len(got))
		for k := range got {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		w := strings.Fields(wantKeys)
		slices.Sort(w)
		if !slices.Equal(keys, w) {
			t.Errorf("%s keys = %v, want %v", name, keys, w)
		}
	}

	readiness := data("metalabel_training_readiness")
	want("readiness", readiness, "as_of rows_from rows_to lookback_days matches_nightly_retrain funnel features last_retrain split")
	want("readiness step", first(readiness, "funnel"), "step condition rows")
	features := obj(readiness, "features")
	want("readiness features", features, "training_rows usable_rows dropped_incomplete missing")
	want("readiness feature", first(features, "missing"), "feature missing_rows")
	last := obj(readiness, "last_retrain")
	want("readiness last retrain", last, "last_run latest_skip latest_model_trained_at dropped_incomplete_status")
	want("readiness latest skip", obj(last, "latest_skip"), "skipped_at reason_code usable_rows required_rows dropped_incomplete")
	split := obj(readiness, "split")
	want("readiness split", split, "llm_cutoff llm_cutoff_status oos_start train_ready train_skip_code train oos")
	want("readiness train", obj(split, "train"), "rows required_rows fit_rows calibration_rows required_fit_rows required_calibration_rows")
	want("readiness oos", obj(split, "oos"), "rows required_rows days required_days required_takes ready")

	models := data("jev_models")
	want("jev models", models, "models daily cutoff")
	want("jev model", first(models, "models"), "model_id decisions trader_decisions first_seen last_seen")
	want("jev model day", first(models, "daily"), "date model_id decisions")
	want("jev model cutoff", obj(models, "cutoff"), "llm_cutoff_model latest_model_id latest_at match")
}
