package paperflow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/paperpolicysettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

// Issue #861: the Paper minimum entry quality defaults to good whatever the
// yaml Live value is, is validated like policy.*.min_entry_quality, is stored
// as a JSON string and resets to good - and never touches the Live keys.
func TestPaperMinEntryQuality_DefaultSaveAndReset(t *testing.T) {
	store := &fakeStore{rows: map[string]string{}}
	live := config.PolicyDirectionThresholds{MinEntryQuality: "strong"} // the shipped Live value
	svc := opsettings.New(store, config.StrategyConfig{Policy: config.PolicyConfig{Long: live, Short: live}}, "/data/logs")
	ctx := context.Background()

	for _, key := range paperpolicysettings.Keys() {
		v, err := svc.Get(ctx, key)
		if err != nil || v.Current != "good" || v.Default != "good" || v.Overridden {
			t.Fatalf("%s = %+v, %v; want good by default (not the yaml strong)", key, v, err)
		}
		for _, bad := range []string{"great", "GOOD", "0.5"} {
			requireInvalid(t, svc.Save(ctx, key, bad))
		}
		if len(store.rows) != 0 {
			t.Fatalf("a rejected value was stored: %v", store.rows)
		}
		if err := svc.Save(ctx, key, " fair "); err != nil {
			t.Fatalf("Save(%s): %v", key, err)
		}
		if store.rows[key] != `"fair"` {
			t.Errorf("stored %s = %q, want the JSON string \"fair\"", key, store.rows[key])
		}
		if v, _ := svc.Get(ctx, key); v.Current != "fair" || !v.Overridden || v.Default != "good" {
			t.Errorf("%s after Save = %+v", key, v)
		}
		if err := svc.Reset(ctx, key); err != nil {
			t.Fatalf("Reset(%s): %v", key, err)
		}
		if v, _ := svc.Get(ctx, key); v.Current != "good" || v.Overridden {
			t.Errorf("%s after Reset = %+v, want good", key, v)
		}
	}
	if v, _ := svc.Get(ctx, "policy.long.min_entry_quality"); v.Current != "strong" || v.Overridden {
		t.Errorf("policy.long.min_entry_quality = %+v, want strong (yaml)", v)
	}
	var invalid *opsettings.InvalidValueError
	if err := svc.Save(ctx, "policy.paper.long.min_probability", "0.5"); !errors.Is(err, opsettings.ErrUnknownKey) && !errors.As(err, &invalid) {
		t.Errorf("Save of a non-existent Paper key = %v, want an unknown-key error", err)
	}
}
