package virtualpaper_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/virtualpaper"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/variant"
	"github.com/ousiassllc/pitha-trador/internal/service/virtualpaper"
)

const rerunDay = "2026-01-06"

// failedDay sets up a profile whose rerunDay replay failed (bars that fail the
// look-ahead check) and returns the profile id.
func failedDay(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	id := create(t, db, "p", at("2026-01-05", 12, 0), variant.Params{})
	if _, err := recorder(db, brokenReplay{}, at("2026-01-07", 16, 30)).Run(context.Background()); err == nil {
		t.Fatal("the broken replay did not fail")
	}
	if st := dayStatus(t, db, id, rerunDay); st != repo.DayFailed {
		t.Fatalf("status = %q, want failed", st)
	}
	return id
}

func dayStatus(t *testing.T, db *sql.DB, id int64, day string) string {
	t.Helper()
	d, err := repo.GetDay(context.Background(), db, id, day)
	if errors.Is(err, repo.ErrDayNotFound) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return d.Status
}

func rerun(db *sql.DB, now time.Time, id int64, day string) error {
	return virtualpaper.RerunFailedDay(context.Background(), db, now, id, day)
}

// A failed day stays failed (the recorder does not loop on it) until it is reset;
// after the reset the next run replays it and the mark becomes done, empty or
// failed again, with nothing but virtual_paper_ tables written.
func TestRerunFailedDay_ReplaysAndMarksAgain(t *testing.T) {
	now := at("2026-01-07", 16, 30)
	for _, c := range []struct {
		name string
		src  virtualpaper.Source
		want string
	}{
		{"done", &daySource{}, repo.DayDone},
		{"empty", &daySource{empty: map[string]bool{rerunDay: true}}, repo.DayEmpty},
		{"failed again", brokenReplay{}, repo.DayFailed},
	} {
		t.Run(c.name, func(t *testing.T) {
			db := openDB(t)
			id := failedDay(t, db)
			// Not reset: the failed day is not replayed again by itself.
			if res, err := recorder(db, &daySource{}, now).Run(context.Background()); err != nil || res.Day != "" {
				t.Fatalf("Run without a reset = %+v, %v; want nothing pending", res, err)
			}
			before := dumpAllBut(t, db, "virtual_paper_")
			if err := rerun(db, now, id, rerunDay); err != nil {
				t.Fatalf("RerunFailedDay: %v", err)
			}
			if st := dayStatus(t, db, id, rerunDay); st != "" {
				t.Fatalf("status after the reset = %q, want no mark (pending)", st)
			}
			res, err := recorder(db, c.src, now).Run(context.Background())
			if (c.want == repo.DayFailed) != (err != nil) || res.Day != rerunDay {
				t.Fatalf("Run = %+v, %v; want the day replayed (failed again: %v)", res, err, c.want == repo.DayFailed)
			}
			if st := dayStatus(t, db, id, rerunDay); st != c.want {
				t.Errorf("status = %q, want %q", st, c.want)
			}
			if c.want == repo.DayFailed {
				// Failed again: still not replayed by itself.
				if res, err := recorder(db, &daySource{}, now).Run(context.Background()); err != nil || res.Day != "" {
					t.Errorf("a failed-again day was replayed without a reset: %+v, %v", res, err)
				}
			}
			if after := dumpAllBut(t, db, "virtual_paper_"); after != before {
				t.Error("the reset and the replay changed a table that is not a virtual_paper_ table")
			}
		})
	}
}

// Only the one failed mark is removed: the profile's other days and the other
// profiles are untouched.
func TestRerunFailedDay_TouchesOnlyThatMark(t *testing.T) {
	db := openDB(t)
	id := failedDay(t, db)
	other := create(t, db, "q", at("2026-01-05", 12, 0), variant.Params{})
	if _, err := recorder(db, &daySource{}, at("2026-01-07", 16, 30)).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Both profiles have a mark of 2026-01-06; other's is done, p's failed.
	if err := rerun(db, at("2026-01-07", 16, 30), id, rerunDay); err != nil {
		t.Fatal(err)
	}
	if st := dayStatus(t, db, other, rerunDay); st != repo.DayDone {
		t.Errorf("the other profile's mark = %q, want done", st)
	}
}

// The refusals: each is an *InvalidError naming the field, none writes anything,
// and none carries a stored value.
func TestRerunFailedDay_Refusals(t *testing.T) {
	db := openDB(t)
	now := at("2026-01-07", 16, 30)
	id := failedDay(t, db)
	done := create(t, db, "done", at("2026-01-05", 12, 0), variant.Params{})
	empty := create(t, db, "empty", at("2026-01-05", 12, 0), variant.Params{})
	stopped := create(t, db, "stopped", at("2026-01-05", 12, 0), variant.Params{})
	for pid, status := range map[int64]string{done: repo.DayDone, empty: repo.DayEmpty} {
		if err := repo.RecordDay(context.Background(), db, repo.Day{ProfileID: pid, Day: rerunDay, Status: status}, nil, now); err != nil {
			t.Fatal(err)
		}
	}
	// stopped has a failed mark but is stopped now.
	if err := repo.RecordDay(context.Background(), db, repo.Day{ProfileID: stopped, Day: rerunDay, Status: repo.DayFailed, Error: "x"}, nil, now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.StopProfile(context.Background(), db, stopped, now); err != nil {
		t.Fatal(err)
	}
	// A failed day too old for the recorder's back-fill window.
	if err := repo.RecordDay(context.Background(), db, repo.Day{ProfileID: id, Day: "2025-12-01", Status: repo.DayFailed, Error: "x"}, nil, now); err != nil {
		t.Fatal(err)
	}
	before := dumpAllBut(t, db, "")
	cases := []struct {
		name  string
		id    int64
		day   string
		field string
		want  string
	}{
		{"done day", done, rerunDay, "day", "done"},
		{"empty day", empty, rerunDay, "day", "empty"},
		{"unknown profile", 999, rerunDay, "profile_id", ""},
		{"unmarked day", id, "2026-01-05", "day", ""},
		{"unknown day", id, "2030-01-01", "day", ""},
		{"not a date", id, "2026-13-40", "day", ""},
		{"garbage day", id, "secret-value", "day", ""},
		{"stopped profile", stopped, rerunDay, "profile_id", ""},
		{"older than the back-fill window", id, "2025-12-01", "day", "10"},
	}
	for _, c := range cases {
		err := rerun(db, now, c.id, c.day)
		var ie *virtualpaper.InvalidError
		if !errors.As(err, &ie) || ie.Field != c.field || !strings.Contains(ie.Reason, c.want) {
			t.Errorf("%s: err = %v, want an InvalidError on %s mentioning %q", c.name, err, c.field, c.want)
			continue
		}
		if strings.Contains(err.Error(), "secret-value") {
			t.Errorf("%s: the error echoes the argument: %v", c.name, err)
		}
	}
	if after := dumpAllBut(t, db, ""); after != before {
		t.Error("a refused reset changed the database")
	}
}
