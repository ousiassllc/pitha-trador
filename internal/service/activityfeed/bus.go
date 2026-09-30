package activityfeed

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// Subscribe registers a new bus subscriber. cancel unregisters it and
// closes the returned channel; call it exactly once.
func (s *Service) Subscribe() (messages <-chan Message, cancel func()) {
	ch := make(chan Message, subscriberBuffer)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
		close(ch)
	}
}

func (s *Service) hasSubscribers() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs) > 0
}

// publish delivers msg to every subscriber without blocking; a subscriber
// whose buffer is full misses it (the writer must never stall on a slow
// WebSocket client).
func (s *Service) publish(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- msg:
		default:
		}
	}
}

// ObserveJob is a jobqueue.JobObserver: it publishes the transition as
// an activity event plus the queue's new depth.
func (s *Service) ObserveJob(ctx context.Context, job jobqueue.Job) {
	if !s.hasSubscribers() {
		return
	}
	ev := jobEvent(job)
	s.publish(Message{Event: &ev})

	counts, err := s.jobs.QueueCounts(ctx, s.now().Add(-FailedWindow))
	if err != nil {
		return
	}
	update := QueueUpdate{Queue: job.Queue}
	for _, c := range counts {
		if c.Queue == job.Queue {
			update.Pending, update.Running, update.FailedRecent = c.Pending, c.Running, c.FailedSince
		}
	}
	s.publish(Message{QueueUpdate: &update})
}

// ObserveDecision is a judgement.DecisionObserver.
func (s *Service) ObserveDecision(_ context.Context, d domain.JevDecision) {
	if !s.hasSubscribers() {
		return
	}
	ev := decisionEvent(d)
	s.publish(Message{Event: &ev})
}

// ObserveKillSwitch is a system.KillSwitchObserver.
func (s *Service) ObserveKillSwitch(_ context.Context, ke domain.KillSwitchEvent) {
	if !s.hasSubscribers() {
		return
	}
	ev := killSwitchEvent(ke)
	s.publish(Message{Event: &ev})
}
