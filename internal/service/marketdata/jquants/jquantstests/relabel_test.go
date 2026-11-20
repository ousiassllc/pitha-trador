package jquantstests

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jquantssettings"
	jq "github.com/ousiassllc/pitha-trador/internal/domain/jquants"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchlabel"
	jqservice "github.com/ousiassllc/pitha-trador/internal/service/marketdata/jquants"
	servicelabel "github.com/ousiassllc/pitha-trador/internal/service/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/service/researchlabel/jquantsrelabel"
)

func fixedCosts(context.Context) (servicelabel.Costs, error) {
	return servicelabel.Costs{Long: 5, Short: 5}, nil
}

// recordDay runs the recorder of the research labels over the one day the rig's
// snapshots cover (09:00 and 09:20 JST: the collector stopped after 09:20).
func recordDay(t *testing.T, r *rig) {
	t.Helper()
	r.seedSnapshot(t, "2026-11-16T00:00:00.000000000Z", 3000, 2999, 3001)
	r.seedSnapshot(t, "2026-11-16T00:20:00.000000000Z", 3010, 3009, 3011)
	rec := servicelabel.NewRecorder(researchlabel.NewRepository(r.db), fixedCosts).WithClock(fixedNow)
	if _, err := rec.Run(context.Background()); err != nil {
		t.Fatalf("recorder: %v", err)
	}
	if n := r.count(t, "research_horizon_labels"); n != 0 {
		t.Fatalf("setup: the recorder labeled %d rows over a day with a hole, want 0", n)
	}
}

func minuteBar(hhmm string, price float64) jq.MinuteBar {
	at, err := time.ParseInLocation("2006-01-02 15:04", backfillDay+" "+hhmm, jst)
	if err != nil {
		panic(err)
	}
	return jq.MinuteBar{Day: backfillDay, Code: "72030", Symbol: "7203", At: at.UTC(), Open: price, High: price, Low: price, Close: price, Volume: 1, Turnover: price}
}

// The collector stopped at 09:20, so the recorder could label nothing: the windows
// needed a bar after it. The stored J-Quants minute bars fill the hole as exits
// only (they have no bid or ask, so they never become an entry): the 09:00 entry
// gets its 30-minute row from the 09:29 bar, and the close row of both entries comes
// from the 15:30 record. Every such row says exit_source = 'jquants'; the day's sums
// are refreshed; nothing else is touched, and a second call adds nothing.
func TestRelabel_FillsTheHoleWithJQuantsExitsAndMarksTheProvenance(t *testing.T) {
	r := newRig(t, testKey, settingsFor(false, true, 100))
	recordDay(t, r)
	if _, err := r.repo.UpsertMinuteBars(context.Background(), []jq.MinuteBar{minuteBar("09:29", 3030), minuteBar("15:30", 3050)}, r.now); err != nil {
		t.Fatal(err)
	}
	relabeler := jquantsrelabel.New(r.labels, r.repo, fixedCosts).WithClock(fixedNow)

	res, err := relabeler.Relabel(context.Background(), backfillDay)
	if err != nil || res.NewRows != 3 || res.Filled != 1 {
		t.Fatalf("Relabel = (%+v, %v), want 3 new rows", res, err)
	}
	rows, err := r.db.Query(`SELECT snapshot_id, horizon, exit_at, exit_price, exit_source, long_gross_bps, long_net_bps, short_gross_bps FROM research_horizon_labels ORDER BY snapshot_id, horizon`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	type row struct {
		snapshot                    int
		horizon, exitAt, source     string
		exitPrice, gross, net, shrt float64
	}
	var got []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.snapshot, &x.horizon, &x.exitAt, &x.exitPrice, &x.source, &x.gross, &x.net, &x.shrt); err != nil {
			t.Fatal(err)
		}
		got = append(got, x)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("rows = %+v, want 3", got)
	}
	for _, x := range got {
		if x.source != "jquants" {
			t.Errorf("row %d/%s has exit_source %q, want jquants", x.snapshot, x.horizon, x.source)
		}
	}
	// Snapshot 1 (09:00, ask 3001): 30m exits at the end of the 09:29 minute (09:29:59 JST = 00:29:59Z, the last moment its close can be known).
	a30 := got[0]
	wantGross := (3030 - 3001.0) / 3001.0 * 10000
	if a30.snapshot != 1 || a30.horizon != "30m" || a30.exitAt != "2026-11-16T00:29:59.000000000Z" || a30.exitPrice != 3030 ||
		math.Abs(a30.gross-wantGross) > 1e-6 || math.Abs(a30.net-(wantGross-5)) > 1e-6 {
		t.Errorf("30m row = %+v, want exit 3030 at 00:29:59Z, gross %v, net %v", a30, wantGross, wantGross-5)
	}
	if want := -(3030 - 2999.0) / 2999.0 * 10000; math.Abs(a30.shrt-want) > 1e-6 {
		t.Errorf("short gross = %v, want %v (entered at the bid)", a30.shrt, want)
	}
	for _, x := range got[1:] {
		if x.horizon != "close" || x.exitAt != "2026-11-16T06:30:00.000000000Z" || x.exitPrice != 3050 {
			t.Errorf("close row = %+v, want the 15:30 record (06:30Z) at 3050", x)
		}
	}
	var l30, lClose int
	if err := r.db.QueryRow(`SELECT labeled_30m, labeled_close FROM research_label_days WHERE day = ?`, backfillDay).Scan(&l30, &lClose); err != nil || l30 != 1 || lClose != 2 {
		t.Errorf("day sums = (%d, %d, %v), want the new rows counted: 1 and 2", l30, lClose, err)
	}

	again, err := relabeler.Relabel(context.Background(), backfillDay)
	if err != nil || again.NewRows != 0 {
		t.Errorf("second Relabel = (%+v, %v), want no new rows", again, err)
	}
	if n := r.count(t, "research_horizon_labels"); n != 3 {
		t.Errorf("rows after the second Relabel = %d, want 3", n)
	}
}

// A day the recorder has not marked done is not re-labeled (the recorder will label it whole).
func TestRelabel_RefusesADayTheRecorderHasNotFinished(t *testing.T) {
	r := newRig(t, testKey, settingsFor(false, true, 100))
	r.seedSnapshot(t, "2026-11-16T00:00:00.000000000Z", 3000, 2999, 3001)
	if _, err := r.repo.UpsertMinuteBars(context.Background(), []jq.MinuteBar{minuteBar("09:29", 3030)}, r.now); err != nil {
		t.Fatal(err)
	}
	_, err := jquantsrelabel.New(r.labels, r.repo, fixedCosts).Relabel(context.Background(), backfillDay)
	if !errors.Is(err, jquantsrelabel.ErrDayNotRecorded) || r.count(t, "research_horizon_labels") != 0 {
		t.Fatalf("Relabel = %v with %d rows, want ErrDayNotRecorded and no rows", err, r.count(t, "research_horizon_labels"))
	}
}

// A label the recorder already wrote is never rewritten: a J-Quants bar inside a
// window the snapshots already cover changes nothing (the snapshot bar is the exit).
func TestRelabel_LeavesRecorderRowsAlone(t *testing.T) {
	r := newRig(t, testKey, settingsFor(false, true, 100))
	r.seedSnapshot(t, "2026-11-16T00:00:00.000000000Z", 3000, 2999, 3001)
	r.seedSnapshot(t, "2026-11-16T00:30:00.000000000Z", 3020, 3019, 3021)
	rec := servicelabel.NewRecorder(researchlabel.NewRepository(r.db), fixedCosts).WithClock(fixedNow)
	if _, err := rec.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := dumpTable(t, r.db, "research_horizon_labels")
	if before == "" {
		t.Fatal("setup: the recorder wrote no 30m row")
	}
	// A J-Quants bar in the same minute as the 09:30 snapshot and one a minute earlier.
	bars := []jq.MinuteBar{minuteBar("09:30", 9999), minuteBar("09:29", 8888)}
	if _, err := r.repo.UpsertMinuteBars(context.Background(), bars, r.now); err != nil {
		t.Fatal(err)
	}
	res, err := jquantsrelabel.New(r.labels, r.repo, fixedCosts).WithClock(fixedNow).Relabel(context.Background(), backfillDay)
	if err != nil {
		t.Fatal(err)
	}
	if after := dumpTable(t, r.db, "research_horizon_labels"); after != before {
		t.Errorf("the recorder's rows changed (new rows %d):\nbefore %s\nafter  %s", res.NewRows, before, after)
	}
}

// Through the backfill: once a day's minute bars are all stored, its research
// labels are re-labeled once, the day gets a relabel mark, and only research label
// rows are written (no jev_* table).
func TestBackfill_RelabelsTheDayAfterItsMinuteBars(t *testing.T) {
	r := newRig(t, testKey, settingsFor(false, true, 100))
	recordDay(t, r)
	relabeler := jquantsrelabel.New(r.labels, r.repo, fixedCosts).WithClock(fixedNow)
	relabel := func(ctx context.Context, day string) (int, bool, error) {
		res, err := relabeler.Relabel(ctx, day)
		if errors.Is(err, jquantsrelabel.ErrDayNotRecorded) {
			return 0, false, nil
		}
		return res.NewRows, err == nil, err
	}
	load := func(ctx context.Context) (jquantssettings.Settings, error) {
		return jquantssettings.Load(ctx, r.settings)
	}
	b := jqservice.NewBackfiller(r.client, r.repo, r.labels, relabel, load, true).WithClock(fixedNow)

	res, err := b.Run(context.Background(), jqservice.Request{})
	if err != nil || len(res.Relabeled) != 1 || res.Relabeled[0] != backfillDay {
		t.Fatalf("Run = (%+v, %v), want the day re-labeled", res, err)
	}
	// minute.json has 09:00, 09:01 and 15:30: the 15:30 record gives both entries a close row.
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM research_horizon_labels WHERE exit_source = 'jquants' AND horizon = 'close'`).Scan(&n); err != nil || n != 2 {
		t.Errorf("close rows from J-Quants = %d (%v), want 2", n, err)
	}
	if got := r.count(t, "research_jquants_coverage"); got < 3 {
		t.Errorf("coverage marks = %d, want symbols, minute and relabel marks", got)
	}
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM research_jquants_coverage WHERE dataset = 'relabel' AND day = ?`, backfillDay).Scan(&n); err != nil || n != 1 {
		t.Errorf("relabel marks = %d (%v), want 1", n, err)
	}
	for _, table := range []string{"jev_decisions", "jev_shadow_decisions", "jev_trial_decisions", "calibration_outcomes"} {
		if got := r.count(t, table); got != 0 {
			t.Errorf("%s has %d rows: the J-Quants run must not write it", table, got)
		}
	}
	before := r.server.count()
	if _, err := b.Run(context.Background(), jqservice.Request{}); err != nil || r.server.count() != before {
		t.Errorf("second run: err %v, %d new requests, want none", err, r.server.count()-before)
	}
}
