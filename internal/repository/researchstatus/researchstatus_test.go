package researchstatus_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/researchstatus"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// A default install (every collection off) writes nothing; a steady state is
// not rewritten; a change is.
func TestSetWritesOnlyWhatChanged(t *testing.T) {
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	r, ctx := researchstatus.New(conn), context.Background()
	t0 := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)

	if err := r.Set(ctx, researchstatus.ComponentBars, researchstatus.StateDisabled, "", t0); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := r.Get(ctx, researchstatus.ComponentBars); ok {
		t.Fatal("disabled with no row was written")
	}
	if err := r.Set(ctx, researchstatus.ComponentBars, researchstatus.StateNotConfigured, "x", t0); err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, researchstatus.ComponentBars, researchstatus.StateNotConfigured, "x", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	st, ok, _ := r.Get(ctx, researchstatus.ComponentBars)
	if !ok || !st.UpdatedAt.Equal(t0) {
		t.Fatalf("an unchanged state was rewritten: %+v %v", st, ok)
	}
	if err := r.Set(ctx, researchstatus.ComponentBars, researchstatus.StateDisabled, "", t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := r.Get(ctx, researchstatus.ComponentBars); st.State != researchstatus.StateDisabled {
		t.Fatalf("switching off after a recorded state must be recorded, got %q", st.State)
	}
}
