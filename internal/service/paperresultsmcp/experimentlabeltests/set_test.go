package experimentlabeltests

import (
	"database/sql"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const hypothesis = "1 回の最大損失を 0.5% に下げると、最大ドローダウンが減る"

// setData is settings_set's data with the optional experiment.
type setData struct {
	Key        string                 `json:"key"`
	Value      string                 `json:"value"`
	Before     *string                `json:"before"`
	AuditID    int64                  `json:"audit_id"`
	At         string                 `json:"at"`
	Experiment *experiment.Experiment `json:"experiment"`
}

func stored(t *testing.T, w *sql.DB, k string) (string, bool) {
	t.Helper()
	var v string
	err := w.QueryRowContext(t.Context(), `SELECT value FROM runtime_settings WHERE key = ?`, k).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return v, true
}

// settings_set with a hypothesis creates exactly one experiment for the change:
// kind setting, name = key, before / after = the change, audit_id = the audit
// row of this very change, started at the write time; the setting is changed.
func TestSetWithHypothesisCreatesOneLinkedExperiment(t *testing.T) {
	srv, w := newServer(t)
	var d setData
	srv.call(t, "settings_set", map[string]any{"key": key, "value": 0.5, "hypothesis": hypothesis}).data(t, "settings_set", &d)

	if d.Experiment == nil {
		t.Fatal("the response has no experiment")
	}
	if n := count(t, w, `SELECT COUNT(*) FROM tuning_experiments`); n != 1 {
		t.Fatalf("tuning_experiments rows = %d, want 1", n)
	}
	v := d.Experiment.Variable
	if v.Kind != experiment.KindSetting || v.Name != key || v.After != "0.5" || v.Before == v.After || v.AuditID == nil || *v.AuditID != d.AuditID {
		t.Errorf("variable = %+v, audit id of the change = %d", v, d.AuditID)
	}
	if d.Experiment.Hypothesis != hypothesis || d.Experiment.Status != experiment.StatusRunning || d.Experiment.Actor != experiment.ActorMCP {
		t.Errorf("experiment = %+v", d.Experiment)
	}
	if d.Experiment.StartedAt != d.At {
		t.Errorf("started_at %s, want the write time %s", d.Experiment.StartedAt, d.At)
	}
	if got, ok := stored(t, w, key); !ok || got != "0.5" {
		t.Errorf("stored value = %q (%v), want 0.5", got, ok)
	}
	// The audit row the experiment points to is the one of this change.
	var auditKey, after string
	if err := w.QueryRowContext(t.Context(), `SELECT setting_key, after_value FROM mcp_settings_audit WHERE id = ?`, d.AuditID).Scan(&auditKey, &after); err != nil || auditKey != key || after != "0.5" {
		t.Errorf("audit row %d: key=%q after=%q err=%v", d.AuditID, auditKey, after, err)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit WHERE setting_key = ?`, key); n != 1 {
		t.Errorf("audit rows of the key = %d, want 1", n)
	}
	// experiments_list shows it, and the change is not "unrecorded".
	var list struct {
		Experiments []experiment.Summary `json:"experiments"`
		Unrecorded  []struct {
			Name string `json:"name"`
		} `json:"unrecorded_changes"`
	}
	srv.call(t, "experiments_list", nil).data(t, "experiments_list", &list)
	if len(list.Experiments) != 1 || list.Experiments[0].Experiment.ID != d.Experiment.ID || len(list.Unrecorded) != 0 {
		t.Errorf("list = %+v", list)
	}
}

// Without a hypothesis nothing changes: the response has the members it always
// had and no experiment is created.
func TestSetWithoutHypothesisIsUnchanged(t *testing.T) {
	srv, w := newServer(t)
	m := srv.call(t, "settings_set", map[string]any{"key": key, "value": 0.5}).members(t, "settings_set")
	want := []string{"at", "audit_id", "before", "key", "unit", "value"}
	if got := slices.Sorted(maps.Keys(m)); !slices.Equal(got, want) {
		t.Errorf("members = %v, want %v", got, want)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM tuning_experiments`); n != 0 {
		t.Errorf("tuning_experiments rows = %d, want 0", n)
	}
	if got, _ := stored(t, w, key); got != "0.5" {
		t.Errorf("stored value = %q", got)
	}
}

// The setting, its audit row and the experiment are one transaction: when the
// experiment cannot be stored, the setting is not changed and no audit row is left.
func TestSetWithHypothesisIsAllOrNothing(t *testing.T) {
	srv, w := newServer(t)
	testkit.MustExec(t, w, `CREATE TRIGGER fail_experiment BEFORE INSERT ON tuning_experiments BEGIN SELECT RAISE(ABORT, 'forced'); END`)
	srv.call(t, "settings_set", map[string]any{"key": key, "value": 0.5, "hypothesis": hypothesis}).wantError(t, http.StatusInternalServerError, paperresultsmcp.CodeInternal)
	if _, ok := stored(t, w, key); ok {
		t.Error("the setting was stored although the experiment failed")
	}
	if n := count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit WHERE setting_key = ?`, key); n != 0 {
		t.Errorf("audit rows = %d, want 0", n)
	}
	testkit.MustExec(t, w, `DROP TRIGGER fail_experiment`)
	srv.call(t, "settings_set", map[string]any{"key": key, "value": 0.5, "hypothesis": hypothesis}).data(t, "settings_set", &setData{})
	if n := count(t, w, `SELECT COUNT(*) FROM tuning_experiments`); n != 1 {
		t.Errorf("experiments after the retry = %d, want 1", n)
	}
}

// A hypothesis that is empty or too long, or a save that changes nothing, is
// refused before anything is written.
func TestSetWithAnInvalidHypothesisWritesNothing(t *testing.T) {
	srv, w := newServer(t)
	srv.call(t, "settings_set", map[string]any{"key": key, "value": 2}).data(t, "settings_set", &setData{})
	audits := count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`)
	for name, args := range map[string]map[string]any{
		"empty":     {"key": key, "value": 0.5, "hypothesis": ""},
		"blank":     {"key": key, "value": 0.5, "hypothesis": "   "},
		"too long":  {"key": key, "value": 0.5, "hypothesis": strings.Repeat("あ", experiment.MaxHypothesisRunes+1)},
		"unchanged": {"key": key, "value": 2, "hypothesis": hypothesis},
	} {
		srv.call(t, "settings_set", args).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
		if got, _ := stored(t, w, key); got != "2" {
			t.Errorf("%s: stored value = %q, want it untouched (2)", name, got)
		}
		if n := count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`); n != audits {
			t.Errorf("%s: audit rows = %d, want %d", name, n, audits)
		}
		if n := count(t, w, `SELECT COUNT(*) FROM tuning_experiments`); n != 0 {
			t.Errorf("%s: experiments = %d, want 0", name, n)
		}
	}
	// A value the Settings validators refuse is still invalid_value, with nothing written.
	srv.call(t, "settings_set", map[string]any{"key": key, "value": 9999, "hypothesis": hypothesis}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
	if n := count(t, w, `SELECT COUNT(*) FROM tuning_experiments`); n != 0 {
		t.Errorf("experiments = %d, want 0", n)
	}
}

// The write switch and the whitelist are checked as before, with a hypothesis too.
func TestSetWithHypothesisKeepsTheWriteSwitchAndTheWhitelist(t *testing.T) {
	srv, w := newServer(t)
	srv.call(t, "settings_set", map[string]any{"key": "min_entry_quality", "value": 1, "hypothesis": hypothesis}).wantError(t, http.StatusForbidden, paperresultsmcp.CodeKeyNotAllowed)
	srv.call(t, "settings_write_disable", map[string]any{}).data(t, "settings_write_disable", &struct{}{})
	audits := count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`)
	srv.call(t, "settings_set", map[string]any{"key": key, "value": 0.5, "hypothesis": hypothesis}).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	srv.call(t, "settings_set", map[string]any{"key": key, "value": 0.5, "hypothesis": ""}).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	if _, ok := stored(t, w, key); ok {
		t.Error("a setting was stored")
	}
	if n := count(t, w, `SELECT COUNT(*) FROM tuning_experiments`); n != 0 {
		t.Errorf("experiments = %d, want 0", n)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`); n != audits {
		t.Errorf("audit rows = %d, want %d", n, audits)
	}
}
