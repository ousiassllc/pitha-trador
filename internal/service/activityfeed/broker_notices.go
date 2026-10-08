package activityfeed

import (
	"context"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/textutil"
)

// maxBrokerNotices bounds the in-memory broker_notice list (like news_feed it
// has no table behind it: lost on restart).
const maxBrokerNotices = 50

// brokerNoticeDetailBytes caps the text of one broker_notice event.
const brokerNoticeDetailBytes = 400

// ObserveBrokerNotice records an operator notice of the broker adapter (立花
// e支店: the morning login overdue, a login fight over the same 認証ID, 書面
// unread, an announced API release or 書面 update date; issue #727) as a
// broker_notice activity event and pushes it to `/ws/activity` subscribers.
// message must already be free of credentials and session URLs. It never
// blocks and never fails.
func (s *Service) ObserveBrokerNotice(message string) {
	ev := domain.ActivityEvent{
		Type:      domain.ActivityTypeBrokerNotice,
		Timestamp: s.now(),
		Detail:    textutil.Excerpt(message, brokerNoticeDetailBytes),
	}
	s.mu.Lock()
	s.brokerNotices = append(s.brokerNotices, ev)
	if len(s.brokerNotices) > maxBrokerNotices {
		s.brokerNotices = slices.Clone(s.brokerNotices[len(s.brokerNotices)-maxBrokerNotices:])
	}
	s.mu.Unlock()

	if s.hasSubscribers() {
		s.publishJob(Message{Event: &ev})
	}
}

// brokerNoticeEvents returns the recorded broker_notice events (oldest first).
func (s *Service) brokerNoticeEvents(_ context.Context) []domain.ActivityEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.brokerNotices)
}
