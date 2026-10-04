package activityfeed

import (
	"context"
	"time"

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
// an activity event and schedules a queue-depth update for the job's
// queue. The depth is not recomputed per transition: it runs on the
// writer's goroutine (enqueue loop, worker), and counting jobs for every
// one of thousands of transitions per minute is needless load. Transitions
// within queueUpdateInterval share one QueueCounts call, run off the
// writer's goroutine (flushQueueUpdates).
func (s *Service) ObserveJob(_ context.Context, job jobqueue.Job) {
	if !s.hasSubscribers() {
		return
	}
	ev := jobEvent(job)
	s.publish(Message{Event: &ev})
	s.markQueueDirty(job.Queue)
}

func (s *Service) markQueueDirty(queue string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dirtyQueues == nil {
		s.dirtyQueues = make(map[string]struct{})
		time.AfterFunc(s.queueUpdateInterval, s.flushQueueUpdates)
	}
	s.dirtyQueues[queue] = struct{}{}
}

// flushQueueUpdates publishes the current depth of every queue marked dirty
// since the last flush, with one QueueCounts call.
func (s *Service) flushQueueUpdates() {
	s.mu.Lock()
	dirty := s.dirtyQueues
	s.dirtyQueues = nil
	s.mu.Unlock()
	if len(dirty) == 0 || !s.hasSubscribers() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), queueUpdateTimeout)
	defer cancel()
	counts, err := s.jobs.QueueCounts(ctx, s.now().Add(-FailedWindow))
	if err != nil {
		return
	}
	byQueue := make(map[string]jobqueue.JobQueueCount, len(counts))
	for _, c := range counts {
		byQueue[c.Queue] = c
	}
	for _, queue := range Queues() {
		if _, ok := dirty[queue]; !ok {
			continue
		}
		c := byQueue[queue]
		s.publish(Message{QueueUpdate: &QueueUpdate{
			Queue: queue, Pending: c.Pending, Running: c.Running, FailedRecent: c.FailedSince,
		}})
	}
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
