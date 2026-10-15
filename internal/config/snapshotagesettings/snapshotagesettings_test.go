package snapshotagesettings_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/snapshotagesettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

type mapStore struct {
	rows map[string]string
	err  error
}

func (m mapStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := m.rows[key]
	return v, ok, m.err
}

func TestDefaultIsTwentySeconds(t *testing.T) {
	if snapshotagesettings.DefaultSeconds != 20 || domain.DefaultMaxSnapshotAge != 20*time.Second {
		t.Fatalf("default = %ds / %v, want 20s (issue #754)", snapshotagesettings.DefaultSeconds, domain.DefaultMaxSnapshotAge)
	}
}

func TestLoad_UnsetUsesDefault(t *testing.T) {
	if got := snapshotagesettings.Load(context.Background(), mapStore{}); got != domain.DefaultMaxSnapshotAge {
		t.Fatalf("Load = %v; want %v", got, domain.DefaultMaxSnapshotAge)
	}
}

func TestLoad_ReadsStoredValue(t *testing.T) {
	if got := snapshotagesettings.Load(context.Background(), mapStore{rows: map[string]string{snapshotagesettings.Key: "45"}}); got != 45*time.Second {
		t.Fatalf("Load = %v; want 45s", got)
	}
}

func TestLoad_DamagedRowFallsBackToDefault(t *testing.T) {
	for _, raw := range []string{"not json", "0", "601", "2.5", `"20"`} {
		if got := snapshotagesettings.Load(context.Background(), mapStore{rows: map[string]string{snapshotagesettings.Key: raw}}); got != domain.DefaultMaxSnapshotAge {
			t.Errorf("Load(%q) = %v; want the default %v", raw, got, domain.DefaultMaxSnapshotAge)
		}
	}
}

func TestLoad_StoreErrorFallsBackToDefault(t *testing.T) {
	store := mapStore{rows: map[string]string{snapshotagesettings.Key: "300"}, err: errors.New("boom")}
	if got := snapshotagesettings.Load(context.Background(), store); got != domain.DefaultMaxSnapshotAge {
		t.Fatalf("Load = %v; want the default %v", got, domain.DefaultMaxSnapshotAge)
	}
}

func TestParse_Range(t *testing.T) {
	for raw, want := range map[string]time.Duration{"1": time.Second, "20": 20 * time.Second, "600": 600 * time.Second} {
		if got, err := snapshotagesettings.Parse(raw); err != nil || got != want {
			t.Errorf("Parse(%q) = %v, %v; want %v", raw, got, err, want)
		}
	}
	for _, raw := range []string{"0", "-5", "601", "1.5", "abc", "null"} {
		if _, err := snapshotagesettings.Parse(raw); err == nil {
			t.Errorf("Parse(%q) accepted, want an error", raw)
		}
	}
}
