// Package newswire picks News Ingest's feed and logs whether it runs. It is
// split out of internal/bootstrap to keep that package within its size limit.
package newswire

import (
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

// NewFeed picks News Ingest's feed (issue #273) and whether it is on.
// The default is the やのしん TDnet WebAPI (no key); NEWS_FEED_URL replaces
// it with the operator's generic feed; NEWS_FEED_ENABLED=off switches the
// feed off (enabled == false, the polling loop is not started). Either feed
// is wrapped in newsfeed.GuardedFeed so a failing feed is queried less and
// less often instead of every minute.
func NewFeed(secrets config.Secrets, yanoshinBaseURL string) (feed newsfeed.Feed, enabled bool) {
	switch {
	case secrets.NewsFeedURL != "":
		feed = newsfeed.NewFeedClient(newsfeed.FeedConfig{URL: secrets.NewsFeedURL, APIKey: secrets.NewsFeedAPIKey})
	default:
		feed = newsfeed.NewYanoshinClient(newsfeed.YanoshinConfig{BaseURL: yanoshinBaseURL})
	}
	return newsfeed.NewGuardedFeed(feed), !secrets.NewsFeedOff()
}

// LogIngest records at start-up why News Ingest is or is not running.
func LogIngest(running, feedOn bool) {
	switch {
	case running:
		slog.Info("bootstrap: news ingest enabled")
	case !feedOn:
		slog.Info("bootstrap: news ingest disabled: NEWS_FEED_ENABLED is off")
	default:
		slog.Warn("bootstrap: news ingest disabled: Luna is unavailable (set JEV_API_KEY, or LUNA_BASE_URL to use another AI)")
	}
}
