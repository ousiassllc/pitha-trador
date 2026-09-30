package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func weeklyNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "weekly"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// Regression (#177): a machine that only runs Monday-Friday still gets
// exactly one weekly archive per ISO week.
func TestBackup_WeekdayOnlyRunsCreateOneWeeklyArchivePerISOWeek(t *testing.T) {
	conn := openTestDB(t)
	dir := t.TempDir()
	s := New(conn, dir, 0)

	for _, day := range []int{28, 29, 30} { // Mon, Tue, Wed of the week of 2026-09-28
		s.now = func() time.Time { return time.Date(2026, 9, day, 16, 0, 0, 0, time.UTC) }
		if err := s.Backup(context.Background()); err != nil {
			t.Fatalf("Backup on 09-%d: %v", day, err)
		}
	}
	if got := weeklyNames(t, dir); len(got) != 1 || got[0] != "pitha-2026-09-28.db.gz" {
		t.Fatalf("weekly archives after one week = %v, want [pitha-2026-09-28.db.gz]", got)
	}

	s.now = func() time.Time { return time.Date(2026, 10, 2, 16, 0, 0, 0, time.UTC) } // Friday of the same week
	if err := s.Backup(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, 10, 6, 16, 0, 0, 0, time.UTC) } // Tuesday, next week
	if err := s.Backup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := weeklyNames(t, dir); len(got) != 2 || got[1] != "pitha-2026-10-05.db.gz" {
		t.Fatalf("weekly archives = %v, want one per ISO week (09-28, 10-05)", got)
	}
}

func TestBackup_WeeklyArchiveWrittenUnderSundayNamingCountsForThatWeek(t *testing.T) {
	conn := openTestDB(t)
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "weekly", "pitha-2026-09-27.db.gz")) // legacy Sunday-named archive
	s := New(conn, dir, 0)
	s.now = func() time.Time { return time.Date(2026, 9, 24, 16, 0, 0, 0, time.UTC) } // Thursday of that ISO week

	if err := s.Backup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := weeklyNames(t, dir); len(got) != 1 {
		t.Fatalf("weekly archives = %v, want the legacy one only", got)
	}
}

func TestIsoWeekMonday(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"2026-09-28", "2026-09-28"}, {"2026-09-27", "2026-09-21"}, {"2026-10-02", "2026-09-28"}, {"2026-01-01", "2025-12-29"},
	} {
		in, _ := time.Parse(dayLayout, c.in)
		if got := isoWeekMonday(in).Format(dayLayout); got != c.want {
			t.Errorf("isoWeekMonday(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}
