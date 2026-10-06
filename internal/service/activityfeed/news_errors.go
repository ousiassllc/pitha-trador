package activityfeed

import (
	"context"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/textutil"
)

// maxNewsErrors bounds the in-memory news_feed error list. Unlike the other
// event types it has no table behind it, so the Service itself keeps the most
// recent errors (lost on restart, like undelivered bus messages).
const maxNewsErrors = 50

// newsErrorDetailBytes caps the error text of one news_feed event.
const newsErrorDetailBytes = 200

// ObserveNewsFeedError records a News Ingest failure (feed fetch or Luna
// classification, issue #273) as a news_feed activity event and pushes it to
// `/ws/activity` subscribers. It is a newsfeed.ErrorObserver: it never
// blocks and never fails, so a failing feed cannot affect the system.
func (s *Service) ObserveNewsFeedError(symbol string, err error) {
	ev := domain.ActivityEvent{
		Type:      domain.ActivityTypeNewsFeed,
		Timestamp: s.now(),
		Symbol:    symbol,
		Detail:    textutil.Excerpt(err.Error(), newsErrorDetailBytes),
	}
	s.mu.Lock()
	s.newsErrors = append(s.newsErrors, ev)
	if len(s.newsErrors) > maxNewsErrors {
		s.newsErrors = slices.Clone(s.newsErrors[len(s.newsErrors)-maxNewsErrors:])
	}
	s.mu.Unlock()

	if s.hasSubscribers() {
		s.publishJob(Message{Event: &ev})
	}
}

// newsErrorEvents returns the recorded news_feed events (oldest first).
func (s *Service) newsErrorEvents(_ context.Context) []domain.ActivityEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.newsErrors)
}
