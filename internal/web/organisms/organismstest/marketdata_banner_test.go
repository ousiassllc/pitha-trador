package organismstest

import (
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// issue #712: the escalated banner's elapsed time is coarsened to 5-minute
// steps so the polite live region is not re-announced on every 30s poll.
func TestMarketDataBanner_PersistentElapsedIsCoarse(t *testing.T) {
	tests := []struct {
		elapsed time.Duration
		want    string
	}{
		{elapsed: 5*time.Minute + 29*time.Second, want: "5分以上継続"},
		{elapsed: 9*time.Minute + 59*time.Second, want: "5分以上継続"},
		{elapsed: 12 * time.Minute, want: "10分以上継続"},
		{elapsed: time.Hour, want: "1時間以上継続"},
		{elapsed: 90*time.Minute + time.Second, want: "1時間30分以上継続"},
	}
	for _, tt := range tests {
		html := renderComponent(t, organisms.MarketDataBanner(organisms.MarketDataBannerProps{
			Issue: "not_logged_in", Guidance: "ログインしてください。", Persistent: true, Failures: 3, Elapsed: tt.elapsed,
		}))
		if !strings.Contains(html, tt.want) {
			t.Errorf("elapsed %v: want %q in %s", tt.elapsed, tt.want, html)
		}
		if strings.Contains(html, "role=") {
			t.Errorf("persistent banner must not carry its own role (issue #626): %s", html)
		}
	}
}

func TestMarketDataBanner_NotPersistentWithoutGuidanceRendersNothing(t *testing.T) {
	html := renderComponent(t, organisms.MarketDataBanner(organisms.MarketDataBannerProps{Persistent: true}))
	if strings.Contains(html, "marketdata-banner") {
		t.Errorf("rendered a banner without guidance: %s", html)
	}
}
