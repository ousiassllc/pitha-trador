package opsflow_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/paperpolicysettings"
)

// Issue #861: the Policy Engine groups show the Paper and the Live minimum
// entry quality side by side, say which one is in use, and the Paper one can
// be changed (stored as a JSON string, so it survives a restart) and reset.
func TestPaperMinEntryQuality_ShownBesideLiveAndSavable(t *testing.T) {
	engine, store := opsRouter()
	body := allPagesBody(t, engine)
	for _, want := range []string{
		"setting-field-row-policy.long.min_entry_quality", "setting-field-row-" + paperpolicysettings.KeyLongMinEntryQuality,
		"setting-field-row-policy.short.min_entry_quality", "setting-field-row-" + paperpolicysettings.KeyShortMinEntryQuality,
		"最低エントリー品質（Paper）", "最低エントリー品質（Live）",
		"いま効いている最低エントリー品質",
		"setting-group-derived-policy-long-effective", "setting-group-derived-policy-short-effective",
		"Paper の値", "Live の値", "判定に使われています", "使われていません",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the Settings pages lack %q", want)
		}
	}

	// Saving a Paper value stores that key only, never the Live one.
	for _, key := range []string{paperpolicysettings.KeyLongMinEntryQuality, paperpolicysettings.KeyShortMinEntryQuality} {
		rec := opsRequest(engine, http.MethodPost, key, url.Values{"value": {"strong"}}, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s: status %d, body %s", key, rec.Code, rec.Body.String())
		}
		if got := store.rows[key]; got != `"strong"` {
			t.Errorf("stored %s = %q, want the JSON string \"strong\"", key, got)
		}
		if _, ok := store.rows[strings.Replace(key, "policy.paper.", "policy.", 1)]; ok {
			t.Errorf("saving %s also stored the Live key", key)
		}
		// The refreshed response carries the panel with the new Paper value.
		if !strings.Contains(rec.Body.String(), "setting-group-derived-") {
			t.Errorf("POST %s did not refresh the status panel: %s", key, rec.Body.String())
		}
	}

	if rec := opsRequest(engine, http.MethodPost, paperpolicysettings.KeyLongMinEntryQuality, url.Values{"value": {"great"}}, true); rec.Code != http.StatusBadRequest {
		t.Errorf("POST an unknown entry quality: status %d, want 400", rec.Code)
	}
	if got := store.rows[paperpolicysettings.KeyLongMinEntryQuality]; got != `"strong"` {
		t.Errorf("a rejected value changed the row to %q", got)
	}

	if rec := opsRequest(engine, http.MethodDelete, paperpolicysettings.KeyLongMinEntryQuality, nil, true); rec.Code != http.StatusOK {
		t.Fatalf("DELETE: status %d, body %s", rec.Code, rec.Body.String())
	}
	if _, ok := store.rows[paperpolicysettings.KeyLongMinEntryQuality]; ok {
		t.Error("reset left the row")
	}
}
