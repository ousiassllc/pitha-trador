package jevtrialroute_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
)

func TestNineArmsAreRejectedAndSaveNothing(t *testing.T) {
	a := newApp(t)
	form := url.Values{"enabled": {"on"}}
	for i := range jevtrialsettings.MaxArms + 1 {
		join(form, armRow(i, "arm-"+strconv.Itoa(i), "m"))
	}
	rec := a.post("/settings/jev-trial", form, true)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "最大 8 件") {
		t.Fatalf("nine arms = %d %s", rec.Code, rec.Body.String())
	}
	for _, key := range jevtrialsettings.Keys() {
		if _, ok := a.stored(key); ok {
			t.Errorf("stored %s", key)
		}
	}
}

// One bad arm among good ones saves none of them and names the bad one in the error slot.
func TestOneInvalidArmSavesNothing(t *testing.T) {
	a := newApp(t)
	form := join(url.Values{"enabled": {"on"}}, validRows, armRow(1, "second", ""))
	rec := a.post("/settings/jev-trial", form, true)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "second") || !strings.Contains(rec.Body.String(), "モデル") {
		t.Fatalf("save = %d %s, want 422 naming the arm and the model", rec.Code, rec.Body.String())
	}
	for _, key := range jevtrialsettings.Keys() {
		if _, ok := a.stored(key); ok {
			t.Errorf("stored %s", key)
		}
	}
}

// Add, edit and delete: add and remove only change the form, the save writes it.
func TestAddEditAndDeleteRows(t *testing.T) {
	a := newApp(t)
	count := func(body string) int { return strings.Count(body, `data-testid="jev-trial-arm-field-row"`) }

	// Add a row to a form that has one typed row (CSRF needed, nothing saved).
	if rec := a.post("/settings/jev-trial/arms/add", validRows, false); rec.Code == http.StatusOK {
		t.Fatalf("add without CSRF = %d", rec.Code)
	}
	rec := a.post("/settings/jev-trial/arms/add", validRows, true)
	if rec.Code != http.StatusOK || count(rec.Body.String()) != 2 || !strings.Contains(rec.Body.String(), `value="or-clef-flash"`) {
		t.Fatalf("add = %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := a.stored(jevtrialsettings.KeyArms); ok {
		t.Fatal("add stored the arms")
	}

	// Save two arms.
	two := join(armRow(0, "or-clef-flash", "cloudflare/clef-flash"), armRow(1, "or-clef", "cloudflare/clef"))
	if rec := a.post("/settings/jev-trial", two, true); rec.Code != http.StatusOK {
		t.Fatalf("save two = %d %s", rec.Code, rec.Body.String())
	}
	if body := a.get("/settings/jev-trial-panel").Body.String(); count(body) != 2 || !strings.Contains(body, `value="or-clef"`) {
		t.Fatalf("panel after save: %s", body)
	}

	// Edit: change the model of the second arm and the cap of the first.
	edited := join(armRow(0, "or-clef-flash", "cloudflare/clef-flash"), armRow(1, "or-clef", "cloudflare/clef-2"), url.Values{"arm-0-daily_cost_cap_usd": {"0.02"}, "arm-0-zdr": {"1"}, "arm-0-provider_only": {"chutes"}})
	if rec := a.post("/settings/jev-trial", edited, true); rec.Code != http.StatusOK {
		t.Fatalf("edit = %d %s", rec.Code, rec.Body.String())
	}
	raw, _ := a.stored(jevtrialsettings.KeyArms)
	arms, err := jevtrialsettings.ParseArms(raw)
	if err != nil || len(arms) != 2 || arms[1].Model != "cloudflare/clef-2" || arms[0].DailyCostCapUSD != 0.02 || !arms[0].ZDR || len(arms[0].ProviderOnly) != 1 {
		t.Fatalf("stored after edit = %q (%v)", raw, err)
	}
	if body := a.get("/settings/jev-trial-panel").Body.String(); !strings.Contains(body, `value="0.02"`) || !strings.Contains(body, `value="chutes"`) {
		t.Errorf("the edited values are not shown: %s", body)
	}

	// Delete the first row: the fragment drops it, the store still holds both until saved.
	rec = a.post("/settings/jev-trial/arms/remove?index=0", edited, true)
	if rec.Code != http.StatusOK || count(rec.Body.String()) != 1 || strings.Contains(rec.Body.String(), `value="or-clef-flash"`) {
		t.Fatalf("remove = %d %s", rec.Code, rec.Body.String())
	}
	if raw, _ := a.stored(jevtrialsettings.KeyArms); !strings.Contains(raw, "or-clef-flash") {
		t.Fatal("remove changed the stored arms")
	}
	if rec := a.post("/settings/jev-trial", armRow(0, "or-clef", "cloudflare/clef-2"), true); rec.Code != http.StatusOK {
		t.Fatalf("save one = %d", rec.Code)
	}
	raw, _ = a.stored(jevtrialsettings.KeyArms)
	if arms, err := jevtrialsettings.ParseArms(raw); err != nil || len(arms) != 1 || arms[0].ID != "or-clef" {
		t.Fatalf("stored after delete = %q (%v)", raw, err)
	}
	// Deleting every row and saving stores no arm.
	if rec := a.post("/settings/jev-trial", url.Values{}, true); rec.Code != http.StatusOK {
		t.Fatalf("save none = %d", rec.Code)
	}
	if raw, _ := a.stored(jevtrialsettings.KeyArms); raw != "[]" {
		t.Errorf("stored = %q, want []", raw)
	}
}

// The key shows only as set / unset: its value is in no response of the section.
func TestTheKeyValueIsNeverInTheHTML(t *testing.T) {
	a := newAppWithKeys(t, fullSecrets{})
	responses := []*httptest.ResponseRecorder{
		a.get("/settings/jev-trial-panel"),
		a.get("/settings/integrations"),
		a.post("/settings/jev-trial", join(url.Values{"enabled": {"on"}}, validRows), true),
		a.post("/settings/jev-trial", join(url.Values{}, armRow(0, "a", "")), true),
		a.post("/settings/jev-trial/arms/add", validRows, true),
	}
	if body := responses[0].Body.String(); !strings.Contains(body, "設定済み") {
		t.Error("the panel does not say the key is set")
	}
	for i, rec := range responses {
		if strings.Contains(rec.Body.String(), keyValue) || strings.Contains(rec.Body.String(), "SECRETVALUE") {
			t.Errorf("response %d carries the key value", i)
		}
	}
}
