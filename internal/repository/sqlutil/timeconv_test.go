package sqlutil_test

import (
	"sort"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// SQLite compares TEXT lexicographically, so FormatTime output must sort
// in chronological order even within a single second (issue #430).
func TestFormatTime_LexicographicOrderIsChronological(t *testing.T) {
	times := []time.Time{
		time.Date(2026, 10, 5, 1, 0, 5, 0, time.UTC),
		time.Date(2026, 10, 5, 1, 0, 5, 1, time.UTC),
		time.Date(2026, 10, 5, 1, 0, 5, 500_000_000, time.UTC),
		time.Date(2026, 10, 5, 1, 0, 5, 510_000_000, time.UTC),
		time.Date(2026, 10, 5, 1, 0, 5, 999_999_999, time.UTC),
		time.Date(2026, 10, 5, 1, 0, 6, 0, time.UTC),
	}
	got := make([]string, len(times))
	for i, tm := range times {
		got[i] = sqlutil.FormatTime(tm)
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("FormatTime outputs are not in lexicographic == chronological order: %q", got)
	}
	if got[0] != "2026-10-05T01:00:05.000000000Z" {
		t.Fatalf("FormatTime(whole second) = %q, want fixed nine-digit fraction", got[0])
	}
}

func TestFormatTime_ConvertsToUTC(t *testing.T) {
	jst := time.FixedZone("JST", 9*60*60)
	got := sqlutil.FormatTime(time.Date(2026, 10, 5, 10, 0, 5, 500_000_000, jst))
	if want := "2026-10-05T01:00:05.500000000Z"; got != want {
		t.Fatalf("FormatTime() = %q, want %q", got, want)
	}
}

// Rows stored before the fixed-width layout carry a variable-width
// fraction; ParseTime must keep reading them alongside the new form.
func TestParseTime_AcceptsLegacyAndFixedWidth(t *testing.T) {
	want := time.Date(2026, 10, 5, 1, 0, 5, 500_000_000, time.UTC)
	for _, in := range []string{
		"2026-10-05T01:00:05.5Z",
		"2026-10-05T01:00:05.500Z",
		"2026-10-05T01:00:05.500000000Z",
	} {
		got, err := sqlutil.ParseTime(in)
		if err != nil || !got.Equal(want) {
			t.Fatalf("ParseTime(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	whole, err := sqlutil.ParseTime("2026-10-05T01:00:05Z")
	if err != nil || !whole.Equal(want.Add(-500*time.Millisecond)) {
		t.Fatalf("ParseTime(legacy whole second) = %v, %v", whole, err)
	}
}

func TestFormatParseTime_RoundTripsNanoseconds(t *testing.T) {
	in := time.Date(2026, 10, 5, 1, 0, 5, 123_456_789, time.UTC)
	got, err := sqlutil.ParseTime(sqlutil.FormatTime(in))
	if err != nil || !got.Equal(in) {
		t.Fatalf("round trip = %v, %v; want %v", got, err, in)
	}
}
