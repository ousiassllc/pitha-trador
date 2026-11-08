package opsflow_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/paperbootstrapsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/regimesettings"
	"github.com/ousiassllc/pitha-trador/internal/config/sleepguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

// onOffKeys are the settings drawn as a switch (issue #1014).
var onOffKeys = []string{paperbootstrapsettings.Key, regimesettings.KeyABEnabled, sleepguardsettings.Key}

// rowHTML returns the markup of key's SettingFieldRow form out of a page body.
func rowHTML(t *testing.T, body, key string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<form[^>]*data-testid="setting-field-row-` + regexp.QuoteMeta(key) + `".*?</form>`).FindString(body)
	if m == "" {
		t.Fatalf("no row for %q in the page", key)
	}
	return m
}

// The three on/off settings reach the page as switches (not text inputs) with
// the default shown as オフ rather than the stored "0".
func TestOnOffSettings_RenderAsSwitchesWithOnOffDefault(t *testing.T) {
	engine, _ := modeRouter(metalabelsettings.ModePaper)
	body := allPagesBody(t, engine)
	for _, key := range onOffKeys {
		row := rowHTML(t, body, key)
		if !strings.Contains(row, `role="switch"`) || strings.Contains(row, `type="text"`) {
			t.Errorf("%s: want a switch and no text input: %s", key, row)
		}
		if !strings.Contains(row, `data-testid="default-`+key+`">既定: オフ<`) {
			t.Errorf("%s: the default must read オフ, not 0: %s", key, row)
		}
	}
}

// getFailingStore reads fail like a database outage while writes still work,
// so a save that wrongly went through would be visible in rows.
type getFailingStore struct{ memRuntimeStore }

func (*getFailingStore) Get(context.Context, string) (string, bool, error) {
	return "", false, errors.New("injected read failure")
}

// When a switch's current value cannot be read it would render OFF; saving
// that would persist an explicit 0 the operator never chose. The page offers
// no 保存 and no hidden 0 for it and says why.
func TestOnOffSettings_UnreadableValueOffersNoSave(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &getFailingStore{memRuntimeStore{rows: map[string]string{}}}
	h := settings.NewSettingsHandler(newFakeSecretsStore()).
		WithOperationalSettings(opsettings.New(store, opsStrategy(), "/data/logs").WithTradingMode(metalabelsettings.ModePaper))
	engine := settingsRouter(h)

	body := allPagesBody(t, engine)
	for _, key := range onOffKeys {
		row := rowHTML(t, body, key)
		if strings.Contains(row, `data-testid="save-setting-`+key+`"`) || strings.Contains(row, `name="value" value="0"`) {
			t.Errorf("%s: an unreadable switch must not be saveable: %s", key, row)
		}
		if !strings.Contains(row, `data-testid="unreadable-`+key+`"`) {
			t.Errorf("%s: the row must say the value could not be read: %s", key, row)
		}
	}
	// A text row keeps its required input and 保存 so an unreadable value can still be replaced on purpose.
	if row := rowHTML(t, body, "screener.top_n"); !strings.Contains(row, `data-testid="save-setting-screener.top_n"`) {
		t.Errorf("a text row must stay saveable: %s", row)
	}
}

// A post that is not exactly 0 or 1 - missing, blank, padded, signed or
// zero-padded - never changes an on/off setting, whatever the form looked like.
func TestSaveOps_OnOffRefusesAnythingButZeroOrOne(t *testing.T) {
	engine, store := modeRouter(metalabelsettings.ModePaper)
	for _, key := range onOffKeys {
		store.rows[key] = "1"
		for name, form := range map[string]url.Values{
			"missing":     {},
			"blank":       {"value": {""}},
			"padded":      {"value": {" 1"}},
			"signed":      {"value": {"+1"}},
			"zero padded": {"value": {"01"}},
			"word":        {"value": {"true"}},
			"two":         {"value": {"2"}},
		} {
			rec := opsRequest(engine, http.MethodPost, key, form, true)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("%s %s: status = %d, want 422; body=%s", key, name, rec.Code, rec.Body.String())
			}
			if store.rows[key] != "1" {
				t.Errorf("%s %s: stored = %q, want the untouched 1", key, name, store.rows[key])
			}
		}
	}
}

// browserSubmission is what a browser posts for the row's form: every
// `value` field in document order, the switch only while checked.
func browserSubmission(t *testing.T, row string, checked bool) url.Values {
	t.Helper()
	form := url.Values{}
	for _, input := range regexp.MustCompile(`<input [^>]*name="value"[^>]*>`).FindAllString(row, -1) {
		value := regexp.MustCompile(`value="([^"]*)"`).FindStringSubmatch(input)[1]
		if strings.Contains(input, `role="switch"`) && !checked {
			continue
		}
		form.Add("value", value)
	}
	return form
}

// The switch is a checkbox followed by a hidden value=0: a checked one posts
// 1 then 0 and an unchecked one only 0. SaveOps reads the FIRST value, so the
// checkbox must come before the hidden field in the real markup; reorder them
// and every checked switch would silently save OFF.
func TestOnOffSettings_CheckedSwitchPostsOneFirst(t *testing.T) {
	engine, store := modeRouter(metalabelsettings.ModePaper)
	body := allPagesBody(t, engine)
	for _, key := range onOffKeys {
		row := rowHTML(t, body, key)
		if strings.Index(row, `role="switch"`) > strings.Index(row, `<input type="hidden" name="value" value="0">`) {
			t.Fatalf("%s: the hidden 0 precedes the switch: %s", key, row)
		}
		if got := browserSubmission(t, row, true); strings.Join(got["value"], ",") != "1,0" {
			t.Fatalf("%s: checked submission = %v, want value=1 then value=0", key, got)
		}
		if rec := opsRequest(engine, http.MethodPost, key, browserSubmission(t, row, true), true); rec.Code != http.StatusOK || store.rows[key] != "1" {
			t.Errorf("%s: checked switch: status = %d, stored = %q, want 1", key, rec.Code, store.rows[key])
		}
		if rec := opsRequest(engine, http.MethodPost, key, browserSubmission(t, row, false), true); rec.Code != http.StatusOK || store.rows[key] != "0" {
			t.Errorf("%s: unchecked switch: status = %d, stored = %q, want 0", key, rec.Code, store.rows[key])
		}
	}
}
