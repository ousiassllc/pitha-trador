package virtualpaper_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/virtualpaper"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/variant"
	"github.com/ousiassllc/pitha-trador/internal/service/virtualpaper"
)

func TestCreate_CapsAndStop(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	now := at("2026-01-05", 12, 0)
	var ids []int64
	for i := range virtualpaper.MaxActiveProfiles {
		ids = append(ids, create(t, db, fmt.Sprintf("p%d", i), now, variant.Params{}))
	}
	_, err := virtualpaper.Create(ctx, db, now, "", virtualpaper.CreateArgs{Name: "extra"})
	var le *virtualpaper.LimitError
	if !errors.As(err, &le) || !strings.Contains(le.Reason, "5") {
		t.Fatalf("the 6th active profile: %v, want a LimitError naming the cap", err)
	}
	// Stopping one frees an active slot but it still counts in the total.
	if _, changed, err := virtualpaper.Stop(ctx, db, now, ids[0]); err != nil || !changed {
		t.Fatalf("Stop = %v, %v", changed, err)
	}
	for i := virtualpaper.MaxActiveProfiles; i < virtualpaper.MaxProfiles; i++ {
		id := create(t, db, fmt.Sprintf("p%d", i), now, variant.Params{})
		if _, _, err := virtualpaper.Stop(ctx, db, now, id); err != nil {
			t.Fatal(err)
		}
	}
	_, err = virtualpaper.Create(ctx, db, now, "", virtualpaper.CreateArgs{Name: "over_total"})
	if !errors.As(err, &le) || !strings.Contains(le.Reason, "20") {
		t.Fatalf("the 21st profile: %v, want a LimitError naming the total cap", err)
	}
}

func TestCreate_NameParamsAndClone(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	now := at("2026-01-05", 12, 0)
	for _, name := range []string{"", strings.Repeat("あ", 41), " lead", "trail ", "tab\tname"} {
		var ie *virtualpaper.InvalidError
		if _, err := virtualpaper.Create(ctx, db, now, "", virtualpaper.CreateArgs{Name: name}); !errors.As(err, &ie) || ie.Field != "name" {
			t.Errorf("name %q: %v, want an InvalidError on name", name, err)
		}
	}
	if _, err := virtualpaper.Create(ctx, db, now, "", virtualpaper.CreateArgs{Name: strings.Repeat("あ", 40)}); err != nil {
		t.Errorf("a 40-character name: %v", err)
	}

	src, err := virtualpaper.Create(ctx, db, now, "", virtualpaper.CreateArgs{Name: "src", Params: variant.Params{TakeProfitPct: ptr(1.0), StopLossPct: ptr(0.8)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := virtualpaper.Stop(ctx, db, now, src.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := virtualpaper.Create(ctx, db, now, "", virtualpaper.CreateArgs{Name: "src"}); !errors.Is(err, repo.ErrNameTaken) {
		t.Errorf("a name of a stopped profile: %v, want ErrNameTaken", err)
	}
	clone, err := virtualpaper.Create(ctx, db, now, "", virtualpaper.CreateArgs{Name: "clone", CloneFrom: &src.ID, Params: variant.Params{TakeProfitPct: ptr(3.0), FeeBps: ptr(2.0)}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"stop_loss_pct":0.8,"take_profit_pct":3,"fee_bps":2}`
	if clone.ParamsJSON != want || clone.ClonedFrom == nil || *clone.ClonedFrom != src.ID || clone.Status != repo.StatusActive {
		t.Errorf("clone = %+v, want params %s cloned from %d", clone, want, src.ID)
	}
	if clone.FirstDay != "2026-01-06" {
		t.Errorf("first_day = %s, want the day after the creation day", clone.FirstDay)
	}

	missing := int64(999)
	var ie *virtualpaper.InvalidError
	if _, err := virtualpaper.Create(ctx, db, now, "", virtualpaper.CreateArgs{Name: "orphan", CloneFrom: &missing}); !errors.As(err, &ie) || ie.Field != "clone_from" {
		t.Errorf("clone of an unknown profile: %v, want an InvalidError on clone_from", err)
	}
	var vi *variant.InvalidError
	if _, err := virtualpaper.Create(ctx, db, now, "", virtualpaper.CreateArgs{Name: "bad", Params: variant.Params{TakeProfitPct: ptr(500.0)}}); !errors.As(err, &vi) || vi.Field != "take_profit_pct" {
		t.Errorf("an out-of-range param: %v, want a variant.InvalidError naming take_profit_pct", err)
	}
	// A clone whose override makes the merge invalid is refused too.
	if _, err := virtualpaper.Create(ctx, db, now, "", virtualpaper.CreateArgs{Name: "bad2", CloneFrom: &src.ID, Params: variant.Params{StopLossPct: ptr(-1.0)}}); !errors.As(err, &vi) {
		t.Errorf("an invalid override of a clone: %v", err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM virtual_paper_profiles WHERE name IN ('bad', 'bad2', 'orphan')`); n != 0 {
		t.Errorf("%d refused profiles were stored", n)
	}
}

func TestStop_IsIdempotentAndKeepsTheData(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	id := create(t, db, "p", at("2026-01-05", 12, 0), variant.Params{})
	runAll(t, recorder(db, &daySource{}, at("2026-01-07", 16, 30)))
	trades := count(t, db, `SELECT COUNT(*) FROM virtual_paper_trades`)

	stoppedAt := at("2026-01-07", 17, 0)
	p, changed, err := virtualpaper.Stop(ctx, db, stoppedAt, id)
	if err != nil || !changed || p.Status != repo.StatusStopped || p.StoppedAt == nil || !p.StoppedAt.Equal(stoppedAt) {
		t.Fatalf("Stop = %+v, %v, %v", p, changed, err)
	}
	if _, changed, err := virtualpaper.Stop(ctx, db, at("2026-01-08", 9, 0), id); err != nil || changed {
		t.Errorf("stopping a stopped profile: changed=%v err=%v, want false and no error", changed, err)
	}
	if _, _, err := virtualpaper.Stop(ctx, db, stoppedAt, 999); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("an unknown id: %v, want ErrNotFound", err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM virtual_paper_trades`); got != trades {
		t.Errorf("stopping changed the recorded trades: %d -> %d", trades, got)
	}

	// A stopped profile is not replayed any more, however many days are pending.
	src := &daySource{}
	runAll(t, recorder(db, src, at("2026-01-12", 16, 30)))
	if len(src.calls) != 0 {
		t.Errorf("a stopped profile was replayed: %v", src.calls)
	}
}
