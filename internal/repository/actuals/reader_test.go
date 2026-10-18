package actuals_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/actuals"
	"github.com/ousiassllc/pitha-trador/internal/repository/actuals/actualstest"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestReader_EmptyDatabase(t *testing.T) {
	r := actuals.NewReader(openDB(t))
	ctx := context.Background()
	if got, err := r.ClosedTrades(ctx); err != nil || len(got) != 0 {
		t.Fatalf("ClosedTrades() = %v, %v; want empty", got, err)
	}
	if got, err := r.JevSamples(ctx); err != nil || len(got) != 0 {
		t.Fatalf("JevSamples() = %v, %v; want empty", got, err)
	}
	if got, err := r.MetaModels(ctx); err != nil || len(got) != 0 {
		t.Fatalf("MetaModels() = %v, %v; want empty", got, err)
	}
}

// ClosedTrades reads the seeded closed positions oldest first with their
// fees, slippage, A/B arm and the NEWEST meta prediction; the open position
// is skipped and the manual entry (no signal) is kept.
func TestReader_ClosedTrades(t *testing.T) {
	db := openDB(t)
	actualstest.Seed(t, db)
	got, err := actuals.NewReader(db).ClosedTrades(context.Background())
	if err != nil {
		t.Fatalf("ClosedTrades: %v", err)
	}
	if len(got) != len(actualstest.Trades) {
		t.Fatalf("got %d trades, want %d (the open position is excluded)", len(got), len(actualstest.Trades))
	}
	for n, want := range actualstest.Trades {
		g := got[n]
		if g.PositionID != int64(want.ID) || g.RealizedPnL != want.Net || g.ExitReason != want.Reason || g.Side != want.Side {
			t.Errorf("trade %d = %+v, want id %d net %v %s %s", n, g, want.ID, want.Net, want.Reason, want.Side)
		}
		if g.EntryFee != want.Fees || g.ExitFee != want.Fees || !g.ClosedAt.Equal(want.ClosedAt) {
			t.Errorf("trade %d fees/closed = %v/%v/%v, want %v/%v/%v", n, g.EntryFee, g.ExitFee, g.ClosedAt, want.Fees, want.Fees, want.ClosedAt)
		}
		if g.ExitPrice == nil || *g.ExitPrice != want.ExitPx {
			t.Errorf("trade %d exit price = %v, want %v", n, g.ExitPrice, want.ExitPx)
		}
	}

	first, fourth, manual := got[0], got[3], got[5]
	if first.ABArm != "momentum" || first.MetaModelVersion != "v-a" || first.MetaExpectedBps == nil || *first.MetaExpectedBps != 40 {
		t.Errorf("trade 1 = arm %q model %q ev %v, want momentum / v-a (the newer model) / 40", first.ABArm, first.MetaModelVersion, first.MetaExpectedBps)
	}
	if first.EntrySlippageBps == nil || *first.EntrySlippageBps != 5 || first.ExitSlippageBps == nil || *first.ExitSlippageBps != 3 {
		t.Errorf("trade 1 slippage = %v/%v, want 5/3", first.EntrySlippageBps, first.ExitSlippageBps)
	}
	if fourth.ABArm != "" || fourth.MetaModelVersion != "" || fourth.MetaExpectedBps != nil || fourth.EntrySlippageBps != nil {
		t.Errorf("trade 4 = %+v, want no arm, no prediction, no slippage", fourth)
	}
	if manual.ABArm != "" || manual.MetaModelVersion != "" {
		t.Errorf("manual trade = %+v, want no decision-derived fields", manual)
	}
}

// JevSamples returns only graded directional decisions and drops ties of both
// label versions (return exactly 0) and NONE decisions.
func TestReader_JevSamples(t *testing.T) {
	db := openDB(t)
	actualstest.Seed(t, db)
	got, err := actuals.NewReader(db).JevSamples(context.Background())
	if err != nil {
		t.Fatalf("JevSamples: %v", err)
	}
	counts := map[int]int{}
	for _, s := range got {
		counts[s.LabelVersion]++
	}
	// v2: decisions 1, 2, 4, 5 (3 is a tie, 6 is NONE); v1: decisions 1, 3 (2 is a tie).
	if counts[2] != 4 || counts[1] != 2 || len(got) != 6 {
		t.Fatalf("samples per label version = %v (total %d), want v1:2 v2:4", counts, len(got))
	}
}

func TestReader_MetaModels(t *testing.T) {
	db := openDB(t)
	actualstest.Seed(t, db)
	got, err := actuals.NewReader(db).MetaModels(context.Background())
	if err != nil {
		t.Fatalf("MetaModels: %v", err)
	}
	if len(got) != 2 || got[0].Version != "v-old" || got[1].Version != "v-a" {
		t.Fatalf("MetaModels() = %+v, want v-old (trained first) then v-a", got)
	}
	a := got[1]
	if a.Status != "active" || a.OOSSamples != 120 || a.OOSAUC == nil || *a.OOSAUC != 0.62 || a.OOSExpectedBps == nil || *a.OOSExpectedBps != 7.5 {
		t.Errorf("v-a = %+v, want active, 120 OOS samples, AUC 0.62, EV 7.5", a)
	}
	if !a.OOSStart.Equal(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("v-a OOSStart = %v", a.OOSStart)
	}
}
