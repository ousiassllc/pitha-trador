package metalabeltests

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

type jevModels struct {
	Models []struct {
		ModelID         string `json:"model_id"`
		Decisions       int64  `json:"decisions"`
		TraderDecisions int64  `json:"trader_decisions"`
		FirstSeen       string `json:"first_seen"`
		LastSeen        string `json:"last_seen"`
	} `json:"models"`
	Daily []struct {
		Date      string `json:"date"`
		ModelID   string `json:"model_id"`
		Decisions int64  `json:"decisions"`
	} `json:"daily"`
	Cutoff struct {
		LLMCutoffModel string  `json:"llm_cutoff_model"`
		LatestModelID  *string `json:"latest_model_id"`
		LatestAt       *string `json:"latest_at"`
		Match          string  `json:"match"`
	} `json:"cutoff"`
}

func callModels(t *testing.T, srv *testServer, args map[string]any) jevModels {
	t.Helper()
	var d jevModels
	srv.call(t, "jev_models", args).data(t, "jev_models", &d)
	return d
}

// decide seeds one decision of kind at ts (RFC3339 UTC) answered by model.
func decide(t *testing.T, w *sql.DB, id int, kind, model, ts string) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash,
		state_json, question_version, response_json, direction, confidence, latency_ms, model_id)
		VALUES (?, 1, '7203', ?, ?, 'h', '{}', 'q1', '{}', 'LONG', 0.7, 5, ?)`, id, testkit.Stored(t, ts), kind, model)
}

// seedModels: jev-1.12.0 answered until the 12th, jev-1.13.0 since (the 12th
// 23:30 UTC is already the 13th in JST, which the daily rows follow).
func seedModels(t *testing.T, w *sql.DB) {
	t.Helper()
	decide(t, w, 1, "trader", "jev-1.12.0", "2026-10-10T01:00:00Z")
	decide(t, w, 2, "scout", "jev-1.12.0", "2026-10-10T02:00:00Z")
	decide(t, w, 3, "trader", "jev-1.12.0", "2026-10-12T23:30:00Z")
	decide(t, w, 4, "trader", "jev-1.13.0", "2026-10-13T01:00:00Z")
	decide(t, w, 5, "trader", "jev-1.13.0", "2026-10-15T05:00:00Z")
	decide(t, w, 6, "scout", "jev-1.13.0", "2026-10-15T06:00:00Z")
	decide(t, w, 7, "trader", "jev-1.13.0", "2026-10-17T00:00:00Z")
}

// Counts, first/last times in JST and the per-day rows.
func TestJevModelsCountsAndDays(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	seedModels(t, w)
	srv := newTestServer(t, path, now)

	d := callModels(t, srv, map[string]any{})
	if len(d.Models) != 2 {
		t.Fatalf("models = %+v", d.Models)
	}
	old, cur := d.Models[0], d.Models[1]
	if old.ModelID != "jev-1.12.0" || old.Decisions != 3 || old.TraderDecisions != 2 ||
		old.FirstSeen != "2026-10-10T10:00:00+09:00" || old.LastSeen != "2026-10-13T08:30:00+09:00" {
		t.Errorf("jev-1.12.0 = %+v", old)
	}
	if cur.ModelID != "jev-1.13.0" || cur.Decisions != 4 || cur.TraderDecisions != 3 ||
		cur.FirstSeen != "2026-10-13T10:00:00+09:00" || cur.LastSeen != "2026-10-17T09:00:00+09:00" {
		t.Errorf("jev-1.13.0 = %+v", cur)
	}
	var days []string
	for _, r := range d.Daily {
		days = append(days, r.Date+" "+r.ModelID+" "+string(rune('0'+r.Decisions)))
	}
	want := []string{"2026-10-10 jev-1.12.0 2", "2026-10-13 jev-1.12.0 1", "2026-10-13 jev-1.13.0 1", "2026-10-15 jev-1.13.0 2", "2026-10-17 jev-1.13.0 1"}
	if strings.Join(days, "|") != strings.Join(want, "|") {
		t.Errorf("daily = %v, want %v", days, want)
	}
}

// from/to narrow the counts; the cutoff comparison always uses the newest
// decision of all (a period before it does not change what is current).
func TestJevModelsPeriod(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	seedModels(t, w)
	testkit.Setting(t, w, metalabelsettings.KeyLLMCutoffModel, `"jev-1.12.0"`)
	srv := newTestServer(t, path, now)

	d := callModels(t, srv, map[string]any{"from": "2026-10-10", "to": "2026-10-12"})
	if len(d.Models) != 1 || d.Models[0].ModelID != "jev-1.12.0" || d.Models[0].Decisions != 2 {
		t.Errorf("models = %+v, want only the two decisions of the 10th", d.Models)
	}
	if d.Cutoff.LatestModelID == nil || *d.Cutoff.LatestModelID != "jev-1.13.0" || d.Cutoff.Match != "mismatch" {
		t.Errorf("cutoff = %+v, want the latest model jev-1.13.0 against jev-1.12.0", d.Cutoff)
	}
	empty := callModels(t, srv, map[string]any{"from": "2026-01-01", "to": "2026-01-05"})
	if len(empty.Models) != 0 || len(empty.Daily) != 0 || empty.Models == nil || empty.Daily == nil {
		t.Errorf("an empty period returns empty lists, got %+v", empty)
	}
}

// match / mismatch / unset, and the exactness of the comparison: the same
// comparison the retraining's cutoff gate applies to the OOS rows.
func TestJevModelsMatchStates(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	srv := newTestServer(t, path, now)

	if got := callModels(t, srv, nil); got.Cutoff.Match != "unset" || got.Cutoff.LatestModelID != nil || got.Cutoff.LatestAt != nil {
		t.Errorf("no decisions: %+v", got.Cutoff)
	}
	seedModels(t, w)
	if got := callModels(t, srv, nil); got.Cutoff.Match != "unset" || got.Cutoff.LLMCutoffModel != "" {
		t.Errorf("no llm_cutoff_model: %+v", got.Cutoff)
	}
	for _, c := range []struct{ stored, match string }{
		{"jev-1.13.0", "match"},
		{"jev-1.12.0", "mismatch"},
		{"jev-1.13", "mismatch"}, // exact, not a prefix
		{"JEV-1.13.0", "mismatch"},
	} {
		testkit.Setting(t, w, metalabelsettings.KeyLLMCutoffModel, `"`+c.stored+`"`)
		got := callModels(t, srv, nil)
		if got.Cutoff.Match != c.match || got.Cutoff.LLMCutoffModel != c.stored ||
			got.Cutoff.LatestModelID == nil || *got.Cutoff.LatestModelID != "jev-1.13.0" ||
			got.Cutoff.LatestAt == nil || *got.Cutoff.LatestAt != "2026-10-17T09:00:00+09:00" {
			t.Errorf("llm_cutoff_model %q: %+v, want %s", c.stored, got.Cutoff, c.match)
		}
		// The shared function gives the same answer for the same two strings.
		if want := training.CutoffModelStatus(c.stored, "jev-1.13.0"); got.Cutoff.Match != want {
			t.Errorf("llm_cutoff_model %q: tool %s, training.CutoffModelStatus %s", c.stored, got.Cutoff.Match, want)
		}
	}
}

// The tool agrees with the real retraining: for the same database, "mismatch"
// is exactly when the Trainer's cutoff gate rejects the OOS rows as
// llm_model_changed, and "match" is when it does not.
func TestJevModelsAgreesWithTheTrainersCutoffGate(t *testing.T) {
	now := testkit.FixedNow()
	for _, c := range []struct {
		cutoffModel, want string
		rejected          bool
	}{
		{"jev-1.13.0", "match", false},
		{"jev-1.13", "mismatch", true},
	} {
		path, w := readinessDB(t)
		learnableHistory(t, w, now) // every row answered by jev-1.13.0
		testkit.Setting(t, w, metalabelsettings.KeyLLMCutoffModel, `"`+c.cutoffModel+`"`)
		testkit.Setting(t, w, metalabelsettings.KeyLLMCutoffConfirmedOn, `"2026-10-01"`)
		testkit.Setting(t, w, metalabelsettings.KeyLLMCutoffSource, `"vendor page"`)

		store := runNightly(t, w, now)
		if store.model == nil {
			t.Fatalf("cutoff model %q: the Trainer stored no model: %v", c.cutoffModel, store.skips)
		}
		gate := store.model.RejectReason == training.RejectLLMModelChanged
		if gate != c.rejected {
			t.Errorf("cutoff model %q: Trainer reject_reason = %q, want llm_model_changed = %v", c.cutoffModel, store.model.RejectReason, c.rejected)
		}
		if got := callModels(t, newTestServer(t, path, now), fullLookback).Cutoff.Match; got != c.want || (got == "mismatch") != gate {
			t.Errorf("cutoff model %q: tool says %s but the Trainer's gate rejected = %v", c.cutoffModel, got, gate)
		}
	}
}

// Nothing secret is read or returned: the response carries only the fields of
// the struct, and the statements never touch runtime_settings outside the
// metalabel.* keys (JEV_MODEL lives encrypted in secrets, never in the DB the
// tool reads).
func TestJevModelsReturnsNoSecrets(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	seedModels(t, w)
	testkit.Setting(t, w, "secret.JEV_MODEL", `"SECRET-VALUE-do-not-leak"`)
	testkit.Setting(t, w, metalabelsettings.KeyLLMCutoffModel, `"jev-1.13.0"`)
	srv := newTestServer(t, path, now)

	for _, tool := range []string{"jev_models", "metalabel_training_readiness"} {
		r := srv.call(t, tool, map[string]any{})
		if strings.Contains(string(r.body), "SECRET-VALUE") || strings.Contains(string(r.body), "JEV_MODEL") || strings.Contains(string(r.body), testAPIKey) {
			t.Errorf("%s leaked a secret: %s", tool, r.body)
		}
	}
	// The latest decision is current as of the call, not of the period.
	if got := callModels(t, srv, map[string]any{"from": "2026-10-10", "to": "2026-10-11"}); got.Cutoff.Match != "match" {
		t.Errorf("cutoff = %+v", got.Cutoff)
	}
}
