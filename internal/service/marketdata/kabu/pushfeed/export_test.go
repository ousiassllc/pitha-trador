package pushfeed

import "time"

// SetClock replaces the clock that stamps and ages PUSH boards; call it
// before Run.
func (f *Feed) SetClock(now func() time.Time) { f.now = now }
