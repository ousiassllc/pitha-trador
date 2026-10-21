package settingstests

import (
	"net/http"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

// Issue #861: the Paper minimum entry quality is written, read back, diffed
// and audited through the MCP, and the Live key is refused. The request and
// response JSON of each step is logged (go test -v) as evidence.
func TestPaperMinEntryQualityThroughTheMCP(t *testing.T) {
	srv, w := settingsServer(t, nil)
	const paperKey, liveKey = "policy.paper.long.min_entry_quality", "policy.long.min_entry_quality"

	// Before any write: the Paper key is listed with its default, good; the
	// Live key is not listed at all.
	st := getState(srv, t)
	it := st.item(t, paperKey)
	if it.Value != "good" || it.Default != "good" || it.Source != "default" || it.Unit != "text" {
		t.Fatalf("%s before a write = %+v, want good/default", paperKey, it)
	}
	for _, i := range st.Items {
		if i.Key == liveKey || i.Key == "policy.short.min_entry_quality" {
			t.Errorf("settings_get lists the Live key %s", i.Key)
		}
	}

	// settings_set -> fair (lower than the default), then back to good.
	req := map[string]any{"key": paperKey, "value": "fair"}
	r := srv.call(t, "settings_set", req)
	t.Logf("settings_set request: %s\nsettings_set response (%d): %s", callBody("settings_set", req), r.status, r.body)
	var out struct {
		Key, Unit, Value string
		Before           *string
	}
	r.data(t, "settings_set", &out)
	if out.Key != paperKey || out.Value != "fair" || out.Unit != "text" {
		t.Errorf("settings_set = %+v", out)
	}
	if v, ok := storedValue(t, w, paperKey); !ok || v != `"fair"` {
		t.Errorf("runtime_settings %s = %q (stored %v), want the JSON string \"fair\"", paperKey, v, ok)
	}

	t.Logf("settings_get request: %s", callBody("settings_get", map[string]any{}))
	st = getState(srv, t)
	t.Logf("settings_get %s item: %+v", paperKey, st.item(t, paperKey))
	if it := st.item(t, paperKey); it.Value != "fair" || it.Source != "stored" || it.Default != "good" {
		t.Errorf("settings_get after the write = %+v", it)
	}

	var diff settingsDiff
	dr := srv.call(t, "settings_diff", map[string]any{})
	t.Logf("settings_diff response (%d): %s", dr.status, dr.body)
	dr.data(t, "settings_diff", &diff)
	if len(diff.Changes) != 1 || diff.Changes[0].Key != paperKey || diff.Changes[0].After.Value != "fair" {
		t.Errorf("settings_diff = %+v, want one change of %s to fair", diff, paperKey)
	}

	// The write is in the audit log.
	var audit auditList
	ar := srv.call(t, "settings_audit", map[string]any{})
	t.Logf("settings_audit response (%d): %s", ar.status, ar.body)
	ar.data(t, "settings_audit", &audit)
	if len(audit.Entries) != 1 || audit.Entries[0].Key != paperKey || audit.Entries[0].After == nil || *audit.Entries[0].After != "fair" {
		t.Errorf("settings_audit = %+v, want the one write of %s", audit.Entries, paperKey)
	}

	// An invalid value is rejected before it is stored.
	bad := set(srv, t, paperKey, "great")
	t.Logf("settings_set invalid value response (%d): %s", bad.status, bad.body)
	bad.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
	if v, _ := storedValue(t, w, paperKey); v != `"fair"` {
		t.Errorf("a rejected write changed %s to %q", paperKey, v)
	}

	// The Live keys are not writable.
	for _, key := range []string{liveKey, "policy.short.min_entry_quality"} {
		req := map[string]any{"key": key, "value": "good"}
		r := srv.call(t, "settings_set", req)
		t.Logf("settings_set request: %s\nsettings_set response (%d): %s", callBody("settings_set", req), r.status, r.body)
		r.wantError(t, http.StatusForbidden, paperresultsmcp.CodeKeyNotAllowed)
		if _, ok := storedValue(t, w, key); ok {
			t.Errorf("the refused write stored %s", key)
		}
	}
}
