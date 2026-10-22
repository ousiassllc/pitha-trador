package db_test

import (
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"testing"

	"github.com/ousiassllc/pitha-trador/db"
)

// golang-migrate applies versions in numeric order and a missing or repeated
// number is a silent hole or an ambiguous version. Several branches add
// migrations at once and meet at merge time, so the embedded migrations are
// checked for a sequence 1..N with no gap and no duplicate, and for exactly one
// up and one down file per number. It holds for any N, so a migration added
// later (the next number) keeps it passing.
func TestMigrationNumbersAreContiguousFromOne(t *testing.T) {
	entries, err := fs.ReadDir(db.MigrationsFS, "migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	name := regexp.MustCompile(`^(\d{6})_[a-z0-9_]+\.(up|down)\.sql$`)
	files := map[int]map[string][]string{} // number -> direction -> file names
	for _, e := range entries {
		m := name.FindStringSubmatch(e.Name())
		if m == nil {
			t.Errorf("%s is not named NNNNNN_title.up.sql / NNNNNN_title.down.sql", e.Name())
			continue
		}
		n, _ := strconv.Atoi(m[1])
		if files[n] == nil {
			files[n] = map[string][]string{}
		}
		files[n][m[2]] = append(files[n][m[2]], e.Name())
	}
	if len(files) == 0 {
		t.Fatal("no migrations found")
	}
	numbers := make([]int, 0, len(files))
	for n := range files {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	for i, n := range numbers {
		if want := i + 1; n != want {
			t.Fatalf("migration numbers are not 1..N without a gap: the %d-th number is %06d, want %06d (numbers %v)", i+1, n, want, numbers)
		}
		for _, dir := range []string{"up", "down"} {
			if got := files[n][dir]; len(got) != 1 {
				t.Errorf("migration %06d has %d %s files %v, want exactly 1", n, len(got), dir, got)
			}
		}
	}
	if last := numbers[len(numbers)-1]; last < 44 {
		t.Errorf("last migration number is %d, want at least 44 (issue #862 adds 000044)", last)
	}
}
