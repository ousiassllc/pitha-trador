package bootstrap

import (
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

// newNewsFeed picks News Ingest's feed (issue #273) and whether it is on.
// The default is the やのしん TDnet WebAPI (no key); NEWS_FEED_URL replaces
// it with the operator's generic feed; NEWS_FEED_ENABLED=off switches the
// feed off (enabled == false, the polling loop is not started). Either feed
// is wrapped in newsfeed.GuardedFeed so a failing feed is queried less and
// less often instead of every minute.
func newNewsFeed(secrets config.Secrets, yanoshinBaseURL string) (feed newsfeed.Feed, enabled bool) {
	switch {
	case secrets.NewsFeedURL != "":
		feed = newsfeed.NewFeedClient(newsfeed.FeedConfig{URL: secrets.NewsFeedURL, APIKey: secrets.NewsFeedAPIKey})
	default:
		feed = newsfeed.NewYanoshinClient(newsfeed.YanoshinConfig{BaseURL: yanoshinBaseURL})
	}
	return newsfeed.NewGuardedFeed(feed), !secrets.NewsFeedOff()
}

// NewsIngestEnabled reports whether Start launches the News Ingest polling
// loop: the feed is not switched off and Luna (Jev or LUNA_*) is available.
func (s *Services) NewsIngestEnabled() bool { return s.newsEnabled }

// logNewsIngest records at start-up why News Ingest is or is not running.
func logNewsIngest(running, feedOn bool) {
	switch {
	case running:
		slog.Info("bootstrap: news ingest enabled")
	case !feedOn:
		slog.Info("bootstrap: news ingest disabled: NEWS_FEED_ENABLED is off")
	default:
		slog.Warn("bootstrap: news ingest disabled: Luna is unavailable (set JEV_API_KEY, or LUNA_BASE_URL to use another AI)")
	}
}
