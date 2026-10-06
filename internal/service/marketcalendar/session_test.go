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
