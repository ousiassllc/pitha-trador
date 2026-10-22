package paperpolicysettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/paperpolicysettings"
)

type mapStore struct {
	rows map[string]string
	err  error
}

func (m mapStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := m.rows[key]
	return v, ok, m.err
}

func TestSettings_Apply(t *testing.T) {
	s := paperpolicysettings.Defaults()
	if s.LongMinEntryQuality != "good" || s.ShortMinEntryQuality != "good" {
		t.Fatalf("Defaults = %+v, want good/good", s)
	}
	if err := s.Apply(paperpolicysettings.KeyShortMinEntryQuality, `"exceptional"`); err != nil {
		t.Fatal(err)
	}
	if s.ShortMinEntryQuality != "exceptional" || s.LongMinEntryQuality != "good" {
		t.Errorf("after Apply short = %+v", s)
	}
	for _, bad := range []string{`"great"`, `"GOOD"`, `""`, `3`, `null`, `not json`} {
		before := s
		if err := s.Apply(paperpolicysettings.KeyLongMinEntryQuality, bad); err == nil {
			t.Errorf("Apply(%s) accepted it", bad)
		}
		if s != before {
			t.Errorf("Apply(%s) changed the settings to %+v", bad, s)
		}
	}
	if err := s.Apply("policy.long.min_entry_quality", `"good"`); err == nil {
		t.Error("Apply accepted the Live key")
	}
}

func TestSource_PaperEntryQuality(t *testing.T) {
	ctx := context.Background()
	got, err := paperpolicysettings.Source{Store: mapStore{rows: map[string]string{}}}.PaperEntryQuality(ctx)
	if err != nil || got != paperpolicysettings.Defaults() {
		t.Fatalf("nothing stored = %+v, %v; want the good defaults", got, err)
	}

	// Stored values apply per side; a damaged row falls back to the default.
	got, err = paperpolicysettings.Source{Store: mapStore{rows: map[string]string{
		paperpolicysettings.KeyLongMinEntryQuality:  `"strong"`,
		paperpolicysettings.KeyShortMinEntryQuality: `"garbage"`,
		"policy.long.min_entry_quality":             `"exceptional"`, // the Live key is never read
	}}}.PaperEntryQuality(ctx)
	if err != nil || got.LongMinEntryQuality != "strong" || got.ShortMinEntryQuality != "good" {
		t.Fatalf("stored = %+v, %v; want long strong, short good", got, err)
	}

	boom := errors.New("db down")
	if _, err := (paperpolicysettings.Source{Store: mapStore{err: boom}}).PaperEntryQuality(ctx); !errors.Is(err, boom) {
		t.Errorf("store failure = %v, want it wrapped", err)
	}
}

func TestPaperKeyOf(t *testing.T) {
	for live, want := range map[string]string{
		"policy.long.min_entry_quality":  paperpolicysettings.KeyLongMinEntryQuality,
		"policy.short.min_entry_quality": paperpolicysettings.KeyShortMinEntryQuality,
	} {
		if got := paperpolicysettings.PaperKeyOf(live); got != want {
			t.Errorf("PaperKeyOf(%q) = %q, want %q", live, got, want)
		}
	}
}
