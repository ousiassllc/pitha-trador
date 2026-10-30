// Package newswire picks News Ingest's feed and logs whether it runs. It is
// split out of internal/bootstrap to keep that package within its size limit.
package newswire

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/safego"
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

// pollInterval is how often News Ingest polls the news feed for the candidates
// and held symbols, in the TSE session only (FR-LUNA-1, #531).
const pollInterval = time.Minute

// RunPoll runs one News Ingest cycle (poll, newsfeed.Service.Poll) every
// pollInterval until ctx is done, starting with an immediate cycle (FR-LUNA-1),
// and calls wg.Done on return. A failed cycle is logged and the loop carries on:
// News Ingest problems must never affect the rest of the system (FR-LUNA-4).
func RunPoll(ctx context.Context, wg *sync.WaitGroup, poll func(context.Context) error) {
	defer wg.Done()

	wait := time.Duration(0) // the first cycle runs immediately
	safego.Loop(ctx, "news ingest", func() time.Duration {
		d := wait
		wait = pollInterval
		return d
	}, poll)
}
