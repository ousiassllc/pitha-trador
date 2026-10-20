package settingstests

import (
	"net/http"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

type settingItem struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Unit    string `json:"unit"`
	Source  string `json:"source"`
	Default string `json:"default"`
	Warning string `json:"warning"`
}

type settingsState struct {
	WriteEnabled bool  `json:"write_enabled"`
	SnapshotID   int64 `json:"snapshot_id"`
	// LLMCutoffStatus is the fixed status code of the LLM cutoff (issue #809).
	LLMCutoffStatus string        `json:"llm_cutoff_status"`
	Items           []settingItem `json:"items"`
}

type settingsDiff struct {
	Since struct {
		ID int64 `json:"id"`
	} `json:"since"`
	SnapshotID int64 `json:"snapshot_id"`
	Changes    []struct {
		Key    string `json:"key"`
		Before struct{ Value, Source string }
		After  struct{ Value, Source string }
	} `json:"changes"`
}

type auditList struct {
	Entries []struct {
		ID     int64   `json:"id"`
		Actor  string  `json:"actor"`
		Client string  `json:"client"`
		Action string  `json:"action"`
		Key    string  `json:"key"`
		Before *string `json:"before"`
		After  *string `json:"after"`
	} `json:"entries"`
}

func getState(srv *testServer, t *testing.T) settingsState {
	t.Helper()
	var st settingsState
	srv.call(t, "settings_get", map[string]any{}).data(t, "settings_get", &st)
	return st
}

func (st settingsState) item(t *testing.T, key string) settingItem {
	t.Helper()
	for _, it := range st.Items {
		if it.Key == key {
			return it
		}
	}
	t.Fatalf("settings_get has no %s", key)
	return settingItem{}
}

// settings_get lists exactly the whitelist, defaults first, writes on by default.
func TestSettingsGetListsTheWhitelistWithDefaults(t *testing.T) {
	srv, _ := settingsServer(t, nil)
	st := getState(srv, t)
	var keys []string
	for _, it := range st.Items {
		keys = append(keys, it.Key)
		if it.Source != "default" || it.Value != it.Default || it.Unit == "" {
			t.Errorf("%s on a fresh database: %+v", it.Key, it)
		}
	}
	if !slices.Equal(keys, mcpwritesettings.Keys()) {
		t.Fatalf("settings_get keys = %v, want the whitelist %v", keys, mcpwritesettings.Keys())
	}
	if !st.WriteEnabled || st.SnapshotID != 0 {
		t.Errorf("fresh state = %+v, want writes on (the default) and snapshot 0", st)
	}
	// The Paper defaults are issue #804's.
	for key, want := range map[string]string{
		"risk.paper.initial_capital_jpy": "500000", "risk.paper.max_position_per_symbol_pct": "100",
		"risk.paper.max_total_exposure_pct": "100", "risk.paper.max_trade_loss_pct": "1",
	} {
		if got := st.item(t, key).Value; got != want {
			t.Errorf("%s default = %s, want %s", key, got, want)
		}
	}
}

// list -> change -> diff, and the value is in the application's own store.
func TestSettingsListChangeDiff(t *testing.T) {
	srv, w := settingsServer(t, nil)
	before := getState(srv, t)

	var out struct {
		Key     string  `json:"key"`
		Value   string  `json:"value"`
		Before  *string `json:"before"`
		AuditID int64   `json:"audit_id"`
	}
	set(srv, t, "risk.paper.max_trade_loss_pct", 2.5).data(t, "settings_set", &out)
	if out.Value != "2.5" || out.Before != nil || out.AuditID != 1 {
		t.Fatalf("settings_set result = %+v", out)
	}
	// Same table, same encoding as the GUI (opsettings stores a JSON number).
	if v, ok := storedValue(t, w, "risk.paper.max_trade_loss_pct"); !ok || v != "2.5" {
		t.Fatalf("runtime_settings value = %q, %v", v, ok)
	}

	after := getState(srv, t)
	it := after.item(t, "risk.paper.max_trade_loss_pct")
	if it.Value != "2.5" || it.Source != "stored" || it.Default != "1" || after.SnapshotID != 1 {
		t.Fatalf("after set: %+v snapshot %d", it, after.SnapshotID)
	}

	// Default diff: vs just before the last save.
	var d settingsDiff
	srv.call(t, "settings_diff", map[string]any{}).data(t, "settings_diff", &d)
	if len(d.Changes) != 1 || d.Changes[0].Key != "risk.paper.max_trade_loss_pct" ||
		d.Changes[0].Before.Value != "1" || d.Changes[0].Before.Source != "default" ||
		d.Changes[0].After.Value != "2.5" || d.Changes[0].After.Source != "stored" {
		t.Fatalf("diff = %+v", d)
	}

	// Diff against the snapshot taken before (the id settings_get reported).
	set(srv, t, "risk.paper.max_trade_loss_pct", 1.5).data(t, "settings_set", &out)
	set(srv, t, "metalabel.llm_cutoff", "2025-06-30").data(t, "settings_set", &out)
	srv.call(t, "settings_diff", map[string]any{"since_id": before.SnapshotID}).data(t, "settings_diff", &d)
	if len(d.Changes) != 2 || d.Changes[0].Key != "risk.paper.max_trade_loss_pct" || d.Changes[0].Before.Value != "1" || d.Changes[0].After.Value != "1.5" ||
		d.Changes[1].Key != "metalabel.llm_cutoff" || d.Changes[1].After.Value != "2025-06-30" {
		t.Fatalf("diff since %d = %+v", before.SnapshotID, d)
	}
	// Diff against the first save's snapshot only reports what changed after it.
	srv.call(t, "settings_diff", map[string]any{"since_id": 1}).data(t, "settings_diff", &d)
	if len(d.Changes) != 2 || d.Changes[0].Before.Value != "2.5" {
		t.Fatalf("diff since 1 = %+v", d)
	}
	// A setting changed back to its starting value is not a difference.
	set(srv, t, "risk.paper.max_trade_loss_pct", 2.5).data(t, "settings_set", &out)
	srv.call(t, "settings_diff", map[string]any{"since_id": 1}).data(t, "settings_diff", &d)
	for _, c := range d.Changes {
		if c.Key == "risk.paper.max_trade_loss_pct" {
			t.Errorf("a setting back at its snapshot value is reported: %+v", c)
		}
	}
	// A snapshot that does not exist, or two selectors, is invalid_argument.
	srv.call(t, "settings_diff", map[string]any{"since_id": 999}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "settings_diff", map[string]any{"since_id": 1, "since_at": "2026-10-18T00:00:00Z"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	// By time: everything at or before FixedNow, i.e. after all rows -> nothing differs.
	srv.call(t, "settings_diff", map[string]any{"since_at": "2026-10-18T03:00:00Z"}).data(t, "settings_diff", &d)
	if len(d.Changes) != 0 {
		t.Errorf("diff since the latest instant = %+v", d)
	}
}

// The audit log says who/when/which key/before/after, newest first, and pages.
func TestSettingsAudit(t *testing.T) {
	srv, _ := settingsServer(t, nil)
	set(srv, t, "risk.operating_capital_jpy", 1000000).data(t, "settings_set", &struct{}{})
	set(srv, t, "risk.operating_capital_jpy", 2000000).data(t, "settings_set", &struct{}{})
	set(srv, t, "metalabel.llm_cutoff_model", "jev-x").data(t, "settings_set", &struct{}{})

	var al auditList
	srv.call(t, "settings_audit", map[string]any{"limit": 2}).data(t, "settings_audit", &al)
	if len(al.Entries) != 2 || al.Entries[0].ID != 3 || al.Entries[1].ID != 2 {
		t.Fatalf("first page = %+v", al.Entries)
	}
	e := al.Entries[1]
	if e.Actor != "mcp" || e.Client != "127.0.0.1" || e.Action != "set" || e.Key != "risk.operating_capital_jpy" ||
		e.Before == nil || *e.Before != "1000000" || e.After == nil || *e.After != "2000000" {
		t.Fatalf("row = %+v", e)
	}
	srv.call(t, "settings_audit", map[string]any{"limit": 2, "before_id": 2}).data(t, "settings_audit", &al)
	if len(al.Entries) != 1 || al.Entries[0].ID != 1 || al.Entries[0].Before != nil {
		t.Fatalf("second page = %+v (first row has no stored before-value)", al.Entries)
	}
	srv.call(t, "settings_audit", map[string]any{"limit": 201}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
}
