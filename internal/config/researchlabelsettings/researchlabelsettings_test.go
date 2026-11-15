package researchlabelsettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/researchlabelsettings"
)

type fakeStore struct {
	rows map[string]string
	err  error
}

func (f fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f.rows[key]
	return v, ok, f.err
}

// Nothing stored means OFF with the 90-day retention: the recording is opt-in.
func TestLoad_DefaultsAreOffAnd90Days(t *testing.T) {
	got, err := researchlabelsettings.Load(context.Background(), fakeStore{})
	if err != nil || got.Enabled || got.RetentionDays != 90 {
		t.Fatalf("Load = %+v, %v; want off and 90 days", got, err)
	}
}

func TestLoad_StoredValuesApplyAndDamagedOnesFallBackToTheDefaults(t *testing.T) {
	ctx := context.Background()
	got, err := researchlabelsettings.Load(ctx, fakeStore{rows: map[string]string{
		researchlabelsettings.KeyEnabled: "1", researchlabelsettings.KeyRetentionDays: "30",
	}})
	if err != nil || !got.Enabled || got.RetentionDays != 30 {
		t.Fatalf("Load = %+v, %v; want on and 30 days", got, err)
	}
	for _, bad := range []string{`"1"`, "true", "null", "", "0.5", "-1"} {
		got, err := researchlabelsettings.Load(ctx, fakeStore{rows: map[string]string{
			researchlabelsettings.KeyEnabled: bad, researchlabelsettings.KeyRetentionDays: bad,
		}})
		if err != nil || got.Enabled || got.RetentionDays != 90 {
			t.Errorf("damaged %q: Load = %+v, %v; want the defaults (off, 90)", bad, got, err)
		}
	}
}

func TestLoad_AReadFailureIsAnErrorNotAGuess(t *testing.T) {
	boom := errors.New("db locked")
	if _, err := researchlabelsettings.Load(context.Background(), fakeStore{err: boom}); !errors.Is(err, boom) {
		t.Fatalf("Load error = %v, want the read failure", err)
	}
}

func TestParseRetentionDays_Range(t *testing.T) {
	for raw, want := range map[string]int{"1": 1, "90": 90, "3650": 3650} {
		if got, err := researchlabelsettings.ParseRetentionDays(raw); err != nil || got != want {
			t.Errorf("ParseRetentionDays(%q) = %d, %v; want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"0", "-1", "3651", "1.5", `"90"`, "null", ""} {
		if _, err := researchlabelsettings.ParseRetentionDays(raw); err == nil {
			t.Errorf("ParseRetentionDays(%q) accepted", raw)
		}
	}
}
