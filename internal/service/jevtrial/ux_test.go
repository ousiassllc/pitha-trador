package jevtrial_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	svc "github.com/ousiassllc/pitha-trador/internal/service/jevtrial"
)

// A rejected arms setting is worded in Japanese for the screen: no settings key, no Go error text.
func TestSave_InvalidArmsAreWordedForTheScreen(t *testing.T) {
	e := newEnv(t)
	for _, arms := range []string{
		`{`, `[{"id":"A","provider":"openrouter","model":"m","prompt_version":"trader-v3"}]`,
		`[{"id":"a","provider":"x","model":"m","prompt_version":"trader-v3"}]`,
		`[{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v3","data_collection":"maybe"}]`,
		`[{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v3","modle":1}]`,
	} {
		err := e.s.Save(context.Background(), svc.Form{ArmsJSON: arms})
		if !errors.Is(err, svc.ErrInvalidInput) {
			t.Fatalf("Save(%q) = %v, want ErrInvalidInput", arms, err)
		}
		msg := strings.TrimPrefix(err.Error(), svc.ErrInvalidInput.Error()+": ")
		if strings.Contains(msg, "jev.trial") || strings.Contains(msg, "jevtrialsettings") || regexp.MustCompile(`\b(decode|must be|unknown field)\b`).MatchString(msg) {
			t.Errorf("Save(%q) message %q carries an internal name or English Go text", arms, msg)
		}
	}
}

// Turning the trial off is written before anything else, so a later failure cannot leave it on.
func TestSave_DisablingIsWrittenFirst(t *testing.T) {
	e := newEnv(t)
	if err := e.s.Save(context.Background(), svc.Form{Enabled: true, ArmsJSON: validArms}); err != nil {
		t.Fatal(err)
	}
	if err := e.s.Save(context.Background(), svc.Form{Enabled: false, ArmsJSON: validArms}); err != nil {
		t.Fatal(err)
	}
	st, err := e.s.State(context.Background())
	if err != nil || st.Enabled {
		t.Fatalf("State = %+v, %v, want the trial off", st, err)
	}
}

// A damaged stored arms setting is shown as it is (so it can be fixed), not as an empty list.
func TestState_DamagedArmsAreShownRaw(t *testing.T) {
	e := newEnv(t)
	if err := e.settings.Set(context.Background(), "jev.trial.arms", `[{"id":"A"`, now); err != nil {
		t.Fatal(err)
	}
	st, err := e.s.State(context.Background())
	if err != nil || st.ArmsError == "" || st.ArmsJSON != `[{"id":"A"` {
		t.Fatalf("State = ArmsJSON %q ArmsError %q (%v), want the stored text and an error", st.ArmsJSON, st.ArmsError, err)
	}
}
