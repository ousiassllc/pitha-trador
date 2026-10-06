package activityfeed

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// subscriber is the bus's per-subscriber state, guarded by Service.mu.
type subscriber struct {
	// lagged is set when job messages were dropped because the channel's
	// job quota was full; the subscriber is owed a Message{Resync: true}.
	lagged bool
}

// Subscribe registers a new bus subscriber. cancel unregisters it and
// closes the returned channel; call it exactly once. The channel is also
// closed by the bus itself when the subscriber falls so far behind that a
// kill switch / decision event cannot be queued (see publishPriority).
func (s *Service) Subscribe() (messages <-chan Message, cancel func()) {
	ch := make(chan Message, jobMessageBuffer+priorityMessageBuffer)
	s.mu.Lock()
	s.subs[ch] = &subscriber{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.removeLocked(ch)
	}
}

// removeLocked unregisters ch and closes it; a no-op when the bus already
// dropped it. s.mu must be held.
func (s *Service) removeLocked(ch chan Message) {
	if _, ok := s.subs[ch]; ok {
		delete(s.subs, ch)
		close(ch)
	}
}

func (s *Service) hasSubscribers() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs) > 0
}

// publishJob delivers a job event or queue update without blocking (the
// writer must never stall on a slow WebSocket client). These are lossy: a
// subscriber already holding jobMessageBuffer messages misses it, and is
// marked lagged so it gets a Resync message once it has room (issue #536).
// Only the job quota is used, so the priorityMessageBuffer headroom stays
// free for publishPriority.
func (s *Service) publishJob(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch, sub := range s.subs {
		s.sendResyncLocked(ch, sub)
		if len(ch) >= jobMessageBuffer {
			sub.lagged = true
			continue
		}
		ch <- msg // room guaranteed: only senders hold s.mu, receivers only free space
	}
}

// publishPriority delivers a kill switch / Jev decision event. It never
// competes with job messages for room. If a subscriber is so far behind
// that even the reserved headroom is full, the event cannot be queued, so
// the subscriber is closed instead of silently losing it: the WebSocket
// handler then ends the connection and the client reconnects and re-fetches
// the snapshot, which contains the event (issue #536).
func (s *Service) publishPriority(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- msg:
		default:
			slog.Warn("activityfeed: subscriber too slow for a priority event; closing it to force a resync")
			s.removeLocked(ch)
		}
	}
}

// sendResyncLocked delivers the owed Resync to a lagged subscriber that
// has room again. s.mu must be held.
func (s *Service) sendResyncLocked(ch chan Message, sub *subscriber) {
	if sub.lagged && len(ch) < jobMessageBuffer {
		ch <- Message{Resync: true}
		sub.lagged = false
	}
}

// resyncLagged delivers pending Resync messages and, while any subscriber
// is still lagged (its buffer has not drained), schedules another flush so
// the notification is not lost when job traffic stops.
func (s *Service) resyncLagged() {
	s.mu.Lock()
	defer s.mu.Unlock()
	stillLagged := false
	for ch, sub := range s.subs {
		s.sendResyncLocked(ch, sub)
		stillLagged = stillLagged || sub.lagged
	}
	if stillLagged {
		s.scheduleFlushLocked()
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
	s.publishJob(Message{Event: &ev})
	s.markQueueDirty(job.Queue)
}

func (s *Service) markQueueDirty(queue string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scheduleFlushLocked()
	s.dirtyQueues[queue] = struct{}{}
}

// scheduleFlushLocked arms the coalescing timer unless one is pending
// (dirtyQueues non-nil). s.mu must be held.
func (s *Service) scheduleFlushLocked() {
	if s.dirtyQueues == nil {
		s.dirtyQueues = make(map[string]struct{})
		time.AfterFunc(s.queueUpdateInterval, s.flushQueueUpdates)
	}
}

// flushQueueUpdates publishes the current depth of every queue marked dirty
// since the last flush, with one QueueCounts call. When that call fails the
// queues stay dirty and are retried after queueUpdateInterval, for as long
// as there are subscribers.
func (s *Service) flushQueueUpdates() {
	defer s.resyncLagged()
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
		// Keep the queues dirty and retry on the next interval: dropping
		// them would leave the queue-depth panel stale until the queue's
		// next transition (issue #420).
		slog.Error("activityfeed: queue counts failed; retrying", "error", err, "queues", len(dirty))
		for queue := range dirty {
			s.markQueueDirty(queue)
		}
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
		s.publishJob(Message{QueueUpdate: &QueueUpdate{
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
	s.publishPriority(Message{Event: &ev})
}

// ObserveKillSwitch is a system.KillSwitchObserver.
func (s *Service) ObserveKillSwitch(_ context.Context, ke domain.KillSwitchEvent) {
	if !s.hasSubscribers() {
		return
	}
	ev := killSwitchEvent(ke)
	s.publishPriority(Message{Event: &ev})
}
