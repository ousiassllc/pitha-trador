package marketcalendar_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

func TestSameSession(t *testing.T) {
	jst := func(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, marketcalendar.JST) }
	tests := []struct {
		name string
		a, b time.Time
		want bool
	}{
		{"within the morning session", jst(6, 9, 0), jst(6, 11, 29), true},
		{"within the afternoon session", jst(6, 12, 30), jst(6, 15, 29), true},
		{"across the lunch break", jst(6, 11, 29), jst(6, 12, 30), false},
		{"previous business day close to next open", jst(5, 15, 29), jst(6, 9, 0), false},
		{"UTC input is read in JST", time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), jst(6, 9, 1), true},
		{"JST midnight boundary", time.Date(2026, 10, 5, 14, 59, 0, 0, time.UTC), time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := marketcalendar.SameSession(tt.a, tt.b); got != tt.want {
				t.Errorf("SameSession(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestSessionStart(t *testing.T) {
	jst := func(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, marketcalendar.JST) }
	tests := []struct {
		name   string
		at     time.Time
		want   time.Time
		wantOK bool
	}{
		{"morning session start", jst(6, 9, 0), jst(6, 9, 0), true},
		{"late morning", jst(6, 11, 29), jst(6, 9, 0), true},
		{"afternoon session start", jst(6, 12, 30), jst(6, 12, 30), true},
		{"late afternoon read from UTC", time.Date(2026, 10, 6, 6, 29, 0, 0, time.UTC), jst(6, 12, 30), true},
		{"before the open", jst(6, 8, 59), time.Time{}, false},
		{"lunch break", jst(6, 12, 0), time.Time{}, false},
		{"at the close", jst(6, 15, 30), time.Time{}, false},
		{"Saturday", jst(10, 10, 0), time.Time{}, false},
		{"Sports Day holiday", jst(12, 10, 0), time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := marketcalendar.TSE.SessionStart(tt.at)
			if ok != tt.wantOK || !got.Equal(tt.want) {
				t.Errorf("SessionStart(%v) = %v, %v; want %v, %v", tt.at, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestSessionsAreTheTwoTradingSessionsOfATradingDay(t *testing.T) {
	got := marketcalendar.TSE.Sessions(time.Date(2026, 10, 22, 23, 0, 0, 0, time.UTC)) // 10-23 08:00 JST
	if len(got) != 2 {
		t.Fatalf("Sessions = %v, want two", got)
	}
	want := [][2]string{{"2026-10-23T09:00", "2026-10-23T11:30"}, {"2026-10-23T12:30", "2026-10-23T15:30"}}
	for i, s := range got {
		if o, c := s.Open.In(marketcalendar.JST).Format("2006-01-02T15:04"), s.Close.In(marketcalendar.JST).Format("2006-01-02T15:04"); o != want[i][0] || c != want[i][1] {
			t.Errorf("session %d = %s-%s, want %v", i, o, c, want[i])
		}
	}
	for _, holiday := range []time.Time{
		time.Date(2026, 10, 12, 10, 0, 0, 0, marketcalendar.JST), // Sports Day
		time.Date(2026, 11, 3, 10, 0, 0, 0, marketcalendar.JST),  // Culture Day
		time.Date(2026, 10, 24, 10, 0, 0, 0, marketcalendar.JST), // Saturday
	} {
		if s := marketcalendar.TSE.Sessions(holiday); s != nil {
			t.Errorf("Sessions(%v) = %v, want none on a non-trading day", holiday, s)
		}
	}
}

func TestPreviousTradingDaySkipsWeekendsAndHolidays(t *testing.T) {
	tests := []struct{ now, want string }{
		{"2026-10-13T10:00:00+09:00", "2026-10-09"}, // after Sports Day weekend
		{"2026-11-04T08:00:00+09:00", "2026-11-02"}, // after Culture Day
		{"2026-10-20T15:45:00+09:00", "2026-10-19"},
		{"2026-10-24T12:00:00+09:00", "2026-10-23"}, // Saturday
	}
	for _, tt := range tests {
		now, err := time.Parse(time.RFC3339, tt.now)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := marketcalendar.TSE.PreviousTradingDay(now)
		if !ok || got.Format(time.DateOnly) != tt.want || got.Hour() != 0 {
			t.Errorf("PreviousTradingDay(%s) = %v, %v; want %s 00:00", tt.now, got, ok, tt.want)
		}
	}
}
