package researchjobsettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
)

type fakeStore struct {
	rows map[string]string
	err  error
}

func (f fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f.rows[key]
	return v, ok, f.err
}

// Nothing stored means both jobs OFF with their default retentions: the recording is opt-in.
func TestDefaultsAreOff(t *testing.T) {
	ev, err := researchjobsettings.Eval(t.Context(), fakeStore{})
	if err != nil || ev.Enabled || ev.RetentionDays != researchjobsettings.DefaultEvalRetentionDays {
		t.Errorf("Eval = %+v, %v; want off with the default retention", ev, err)
	}
	m, err := researchjobsettings.Model(t.Context(), fakeStore{})
	if err != nil || m.Enabled || m.RetentionDays != researchjobsettings.DefaultModelRetentionDays {
		t.Errorf("Model = %+v, %v; want off with the default retention", m, err)
	}
}

// The two jobs read their own keys: turning one on does not turn the other on.
func TestSwitchesAreIndependent(t *testing.T) {
	st := fakeStore{rows: map[string]string{researchjobsettings.KeyEvalEnabled: "1", researchjobsettings.KeyEvalRetentionDays: "30"}}
	ev, _ := researchjobsettings.Eval(t.Context(), st)
	m, _ := researchjobsettings.Model(t.Context(), st)
	if !ev.Enabled || ev.RetentionDays != 30 || m.Enabled {
		t.Errorf("eval = %+v, model = %+v; want eval on (30 days) and model off", ev, m)
	}
}

// A damaged stored value never switches a job on or shortens the retention.
func TestDamagedValuesFallBackToDefaults(t *testing.T) {
	st := fakeStore{rows: map[string]string{
		researchjobsettings.KeyModelEnabled: "2", researchjobsettings.KeyModelRetentionDays: "0",
		researchjobsettings.KeyEvalEnabled: `"yes"`, researchjobsettings.KeyEvalRetentionDays: "1.5",
	}}
	m, _ := researchjobsettings.Model(t.Context(), st)
	ev, _ := researchjobsettings.Eval(t.Context(), st)
	if m.Enabled || m.RetentionDays != researchjobsettings.DefaultModelRetentionDays || ev.Enabled || ev.RetentionDays != researchjobsettings.DefaultEvalRetentionDays {
		t.Errorf("model = %+v, eval = %+v; want the defaults", m, ev)
	}
}

func TestFailedReadIsAnError(t *testing.T) {
	boom := errors.New("boom")
	if _, err := researchjobsettings.Eval(t.Context(), fakeStore{err: boom}); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the store's error", err)
	}
}

func TestValidate(t *testing.T) {
	for key, bad := range map[string][]string{
		researchjobsettings.KeyEvalEnabled:        {"2", "-1", "null", `"1"`, "0.5"},
		researchjobsettings.KeyModelRetentionDays: {"0", "3651", "null", "1.5", "abc"},
		researchjobsettings.KeyEvalRetentionDays:  {"0"},
		researchjobsettings.KeyModelEnabled:       {"true"},
	} {
		for _, v := range bad {
			if researchjobsettings.Validate(key, v) == nil {
				t.Errorf("Validate(%s, %s) accepted a bad value", key, v)
			}
		}
	}
	for key, v := range map[string]string{researchjobsettings.KeyEvalEnabled: "1", researchjobsettings.KeyModelEnabled: "0", researchjobsettings.KeyEvalRetentionDays: "3650", researchjobsettings.KeyModelRetentionDays: "1"} {
		if err := researchjobsettings.Validate(key, v); err != nil {
			t.Errorf("Validate(%s, %s) = %v", key, v, err)
		}
	}
}
