package marketcalendar_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

func jst(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, marketcalendar.JST)
}

func TestIsOpen(t *testing.T) {
	tests := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"before morning open", jst(2026, 9, 29, 8, 59), false},
		{"morning open inclusive", jst(2026, 9, 29, 9, 0), true},
		{"last minute of morning", jst(2026, 9, 29, 11, 29), true},
		{"morning close exclusive", jst(2026, 9, 29, 11, 30), false},
		{"lunch break", jst(2026, 9, 29, 12, 0), false},
		{"afternoon open inclusive", jst(2026, 9, 29, 12, 30), true},
		{"last minute of afternoon", jst(2026, 9, 29, 15, 29), true},
		{"afternoon close exclusive", jst(2026, 9, 29, 15, 30), false},
		{"night", jst(2026, 9, 29, 23, 0), false},
		{"saturday midday", jst(2026, 10, 3, 10, 0), false},
		{"sunday midday", jst(2026, 10, 4, 10, 0), false},
		{"national holiday (成人の日)", jst(2026, 1, 12, 10, 0), false},
		{"year-end closure", jst(2025, 12, 31, 10, 0), false},
		{"new-year closure 1/2", jst(2026, 1, 2, 10, 0), false},
		{"first trading day of year", jst(2026, 1, 5, 10, 0), true},
		{"UTC input converted to JST (00:30Z = 09:30 JST)", time.Date(2026, 9, 29, 0, 30, 0, 0, time.UTC), true},
		{"UTC input converted to JST (23:30Z Mon = 08:30 JST Tue)", time.Date(2026, 9, 28, 23, 30, 0, 0, time.UTC), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := marketcalendar.TSE.IsOpen(tt.at); got != tt.want {
				t.Fatalf("IsOpen(%v) = %v, want %v", tt.at, got, tt.want)
			}
		})
	}
}

func TestIsTradingDayHolidays(t *testing.T) {
	closed := []time.Time{
		jst(2026, 1, 12, 0, 0), // 成人の日
		jst(2026, 2, 11, 0, 0), // 建国記念の日
		jst(2026, 2, 23, 0, 0), // 天皇誕生日
		jst(2026, 3, 20, 0, 0), // 春分の日
		jst(2026, 4, 29, 0, 0), // 昭和の日
		jst(2026, 5, 6, 0, 0),  // 振替休日 (5/3 is Sunday)
		jst(2026, 7, 20, 0, 0), // 海の日
		jst(2026, 8, 11, 0, 0), // 山の日
		jst(2026, 9, 21, 0, 0), // 敬老の日
		jst(2026, 9, 22, 0, 0), // 国民の休日
		jst(2026, 9, 23, 0, 0), // 秋分の日
		jst(2026, 10, 12, 0, 0),
		jst(2026, 11, 3, 0, 0),
		jst(2026, 11, 23, 0, 0),
		jst(2025, 2, 24, 0, 0), // 振替休日 (2/23 is Sunday)
		jst(2024, 9, 23, 0, 0), // 振替休日 (9/22 is Sunday)
		jst(2020, 7, 23, 0, 0), // 海の日 (Olympic relocation)
		jst(2020, 7, 24, 0, 0), // スポーツの日 (Olympic relocation)
		jst(2021, 8, 9, 0, 0),  // 振替休日 (山の日 8/8 is Sunday)
	}
	for _, d := range closed {
		if marketcalendar.TSE.IsTradingDay(d) {
			t.Errorf("%s should be closed", d.Format("2006-01-02"))
		}
	}
	trading := []time.Time{
		jst(2021, 10, 11, 0, 0), // スポーツの日 moved to July in 2020/2021
		jst(2026, 9, 29, 0, 0),
		jst(2026, 9, 24, 0, 0),
		jst(2026, 5, 7, 0, 0),
		jst(2026, 10, 13, 0, 0),
	}
	for _, d := range trading {
		if !marketcalendar.TSE.IsTradingDay(d) {
			t.Errorf("%s should be a trading day", d.Format("2006-01-02"))
		}
	}
}

func TestOpenAtAndCloseAt(t *testing.T) {
	at := jst(2026, 9, 29, 13, 45)
	closeAt, ok := marketcalendar.TSE.CloseAt(at)
	if !ok || !closeAt.Equal(jst(2026, 9, 29, 15, 30)) {
		t.Fatalf("CloseAt = %v, %v; want 15:30 JST", closeAt, ok)
	}
	openAt, ok := marketcalendar.TSE.OpenAt(at)
	if !ok || !openAt.Equal(jst(2026, 9, 29, 9, 0)) {
		t.Fatalf("OpenAt = %v, %v; want 09:00 JST", openAt, ok)
	}
	// Off-hours on a trading day still resolve that date's close.
	closeAt, ok = marketcalendar.TSE.CloseAt(jst(2026, 9, 29, 20, 0))
	if !ok || !closeAt.Equal(jst(2026, 9, 29, 15, 30)) {
		t.Fatalf("evening CloseAt = %v, %v", closeAt, ok)
	}
	if _, ok := marketcalendar.TSE.CloseAt(jst(2026, 10, 3, 10, 0)); ok {
		t.Fatal("CloseAt on a Saturday must report no session")
	}
	if _, ok := marketcalendar.TSE.OpenAt(jst(2026, 1, 12, 10, 0)); ok {
		t.Fatal("OpenAt on a national holiday must report no session")
	}
}

func TestNextOpen(t *testing.T) {
	tests := []struct {
		name string
		at   time.Time
		want time.Time
	}{
		{"before morning open", jst(2026, 9, 29, 8, 0), jst(2026, 9, 29, 9, 0)},
		{"exactly at morning open is not strictly after", jst(2026, 9, 29, 9, 0), jst(2026, 9, 29, 12, 30)},
		{"during morning session", jst(2026, 9, 29, 10, 0), jst(2026, 9, 29, 12, 30)},
		{"lunch break", jst(2026, 9, 29, 12, 0), jst(2026, 9, 29, 12, 30)},
		{"during afternoon session", jst(2026, 9, 29, 14, 0), jst(2026, 9, 30, 9, 0)},
		{"after close", jst(2026, 9, 29, 15, 30), jst(2026, 9, 30, 9, 0)},
		{"friday after close skips weekend", jst(2026, 10, 2, 15, 30), jst(2026, 10, 5, 9, 0)},
		{"saturday night", jst(2026, 10, 3, 21, 0), jst(2026, 10, 5, 9, 0)},
		{"sunday", jst(2026, 10, 4, 10, 0), jst(2026, 10, 5, 9, 0)},
		{"friday before a holiday Monday", jst(2026, 10, 9, 16, 0), jst(2026, 10, 13, 9, 0)},
		{"year-end closure", jst(2026, 12, 30, 16, 0), jst(2027, 1, 4, 9, 0)},
		{"new year day", jst(2027, 1, 1, 10, 0), jst(2027, 1, 4, 9, 0)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := marketcalendar.TSE.NextOpen(tc.at)
			if !got.Equal(tc.want) || got.Location() != marketcalendar.JST {
				t.Errorf("NextOpen(%v) = %v, want %v (JST)", tc.at, got, tc.want)
			}
		})
	}
}

func TestNextOpenNormalisesZone(t *testing.T) {
	// Saturday 12:00 UTC is 21:00 JST; the JST date, not the UTC one, decides.
	got := marketcalendar.TSE.NextOpen(time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC))
	if want := jst(2026, 10, 5, 9, 0); !got.Equal(want) {
		t.Errorf("NextOpen = %v, want %v", got, want)
	}
}

func TestNextOpenIsAlwaysAnOpenSession(t *testing.T) {
	for at := jst(2026, 12, 20, 0, 0); at.Before(jst(2027, 1, 10, 0, 0)); at = at.Add(97 * time.Minute) {
		next := marketcalendar.TSE.NextOpen(at)
		if !next.After(at) || !marketcalendar.TSE.IsOpen(next) || marketcalendar.TSE.IsOpen(next.Add(-time.Minute)) {
			t.Fatalf("NextOpen(%v) = %v is not a session start after t", at, next)
		}
	}
}
