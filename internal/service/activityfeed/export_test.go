package activityfeed

import "time"

// SetQueueUpdateInterval shortens the ObserveJob coalescing window for tests.
func (s *Service) SetQueueUpdateInterval(d time.Duration) { s.queueUpdateInterval = d }
