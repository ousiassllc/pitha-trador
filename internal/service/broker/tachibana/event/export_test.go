package event

// WakePending reports whether a watch list change is still waiting for the
// loop to take it.
func (f *Feed) WakePending() bool { return len(f.wake) > 0 }
