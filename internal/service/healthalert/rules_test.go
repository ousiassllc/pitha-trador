package healthalert_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/healthalert"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// at is a JST instant on 2026-10-<day>: the 20th is a Tuesday trading day, the
// 24th a Saturday, the 12th the Sports Day holiday.
func at(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, marketcalendar.JST) }

func TestKabuNotLoggedInSince(t *testing.T) {
	open := at(20, 8, 45)
	tests := []struct {
		name       string
		now        time.Time
		streak     time.Time
		wantSince  time.Time
		wantRaised bool
	}{
		{"not logged in at 08:45 raises at once, since 08:45", at(20, 8, 45), at(20, 8, 0), open, true},
		{"a streak from the previous evening counts as present at 08:45", at(20, 9, 30), at(19, 20, 0), open, true},
		{"a streak that began exactly at 08:45 counts", at(20, 8, 46), open, open, true},
		{"before 08:45 nothing is raised however long the streak", at(20, 8, 44), at(19, 20, 0), time.Time{}, false},
		{"a streak that began after 08:45 waits five minutes", at(20, 10, 4), at(20, 10, 0), time.Time{}, false},
		{"... and is raised at five minutes, since its start", at(20, 10, 5), at(20, 10, 0), at(20, 10, 0), true},
		{"it is still raised during the lunch break", at(20, 12, 0), at(20, 8, 0), open, true},
		{"it is still raised after the afternoon open", at(20, 15, 29), at(20, 14, 0), at(20, 14, 0), true},
		{"nothing at the close", at(20, 15, 30), at(20, 8, 0), time.Time{}, false},
		{"nothing when kabu is logged in", at(20, 10, 0), time.Time{}, time.Time{}, false},
		{"nothing on a Saturday", at(24, 10, 0), at(24, 8, 0), time.Time{}, false},
		{"nothing on a holiday", at(12, 10, 0), at(12, 8, 0), time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			since, ok := healthalert.KabuNotLoggedInSince(tt.now, tt.streak)
			if ok != tt.wantRaised || !since.Equal(tt.wantSince) {
				t.Errorf("KabuNotLoggedInSince(%v, %v) = %v, %v; want %v, %v", tt.now, tt.streak, since, ok, tt.wantSince, tt.wantRaised)
			}
		})
	}
}

func TestIdleSince(t *testing.T) {
	tests := []struct {
		name       string
		now        time.Time
		last       time.Time
		wantSince  time.Time
		wantRaised bool
	}{
		{"five minutes after the last job", at(20, 10, 5), at(20, 10, 0), at(20, 10, 0), true},
		{"4m59s is not yet", time.Date(2026, 10, 20, 10, 4, 59, 0, marketcalendar.JST), at(20, 10, 0), time.Time{}, false},
		{"yesterday's job: counted from the open", at(20, 9, 5), at(19, 15, 0), at(20, 9, 0), true},
		{"first minutes of the session are no alarm", at(20, 9, 4), at(19, 15, 0), time.Time{}, false},
		{"no job at all, counted from the open", at(20, 9, 5), time.Time{}, at(20, 9, 0), true},
		{"after lunch counted from 12:30, not the morning's last job", at(20, 12, 34), at(20, 11, 20), time.Time{}, false},
		{"after lunch raised five minutes after 12:30", at(20, 12, 35), at(20, 11, 20), at(20, 12, 30), true},
		{"lunch break", at(20, 12, 0), at(20, 9, 0), time.Time{}, false},
		{"before the open", at(20, 8, 50), time.Time{}, time.Time{}, false},
		{"at the close", at(20, 15, 30), at(20, 9, 0), time.Time{}, false},
		{"Saturday", at(24, 10, 0), time.Time{}, time.Time{}, false},
		{"holiday", at(12, 10, 0), time.Time{}, time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			since, ok := healthalert.IdleSince(tt.now, tt.last)
			if ok != tt.wantRaised || !since.Equal(tt.wantSince) {
				t.Errorf("IdleSince(%v, %v) = %v, %v; want %v, %v", tt.now, tt.last, since, ok, tt.wantSince, tt.wantRaised)
			}
		})
	}
}

func TestInWindow(t *testing.T) {
	kabu, idle := domain.HealthAlertKabuNotLoggedIn, domain.HealthAlertMarketDataIdle
	tests := []struct {
		code string
		now  time.Time
		want bool
	}{
		{kabu, at(20, 8, 44), false}, {kabu, at(20, 8, 45), true}, {kabu, at(20, 12, 0), true}, {kabu, at(20, 15, 30), false},
		{idle, at(20, 8, 59), false}, {idle, at(20, 9, 0), true}, {idle, at(20, 12, 0), false}, {idle, at(20, 15, 30), false},
		{kabu, at(24, 10, 0), false}, {idle, at(24, 10, 0), false},
		{"free text from nowhere", at(20, 10, 0), false},
	}
	for _, tt := range tests {
		if got := healthalert.InWindow(tt.code, tt.now); got != tt.want {
			t.Errorf("InWindow(%q, %v) = %v, want %v", tt.code, tt.now, got, tt.want)
		}
	}
}
