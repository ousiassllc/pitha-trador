package virtualpaper_test

import (
	"context"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest/variant"
)

// With no active profile (the default) a Run loads nothing and writes nothing: the
// whole database is byte-for-byte what it was.
func TestRun_WithoutActiveProfilesDoesNothing(t *testing.T) {
	db := openDB(t)
	before := dumpAllBut(t, db, "")
	src := &daySource{}
	res, err := recorder(db, src, at("2026-01-07", 16, 30)).Run(context.Background())
	if err != nil || res.Active != 0 || res.Day != "" || res.More {
		t.Fatalf("Run = %+v, %v; want an idle result", res, err)
	}
	if len(src.calls) != 0 {
		t.Errorf("the source was read: %v", src.calls)
	}
	if after := dumpAllBut(t, db, ""); after != before {
		t.Error("a Run without profiles changed the database")
	}
}

// The recorder writes only the virtual_paper_* tables: every other table (orders,
// positions, signals, the settings and the risk tables included) is unchanged by
// a recording of several profiles over several days.
func TestRun_WritesOnlyTheVirtualPaperTables(t *testing.T) {
	db := openDB(t)
	for _, name := range []string{"a", "b", "c"} {
		p := variant.Params{}
		if name == "b" {
			p.TakeProfitPct = ptr(1.0)
		}
		create(t, db, name, at("2026-01-05", 12, 0), p)
	}
	before := dumpAllBut(t, db, "virtual_paper_")
	runAll(t, recorder(db, &daySource{}, at("2026-01-09", 16, 30)))
	if got := count(t, db, `SELECT COUNT(*) FROM virtual_paper_trades`); got == 0 {
		t.Fatal("nothing was recorded: the test proves nothing")
	}
	if after := dumpAllBut(t, db, "virtual_paper_"); after != before {
		t.Error("the recorder changed a table that is not a virtual_paper_ table")
	}
	for _, tbl := range []string{"paper_orders", "positions", "trade_signals", "runtime_settings"} {
		if n := count(t, db, `SELECT COUNT(*) FROM `+tbl); n != 0 {
			t.Errorf("%s has %d rows after a virtual Paper recording", tbl, n)
		}
	}
}
