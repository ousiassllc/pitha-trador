package disclosureskiptests

import (
	"testing"
	"time"
)

// The recording feed stores a fetch asynchronously. A cycle that runs before
// that write has landed finds no covering fetch in the database, but must not
// ask やのしん for the same symbol again: one decision, one request.
func TestFollowup_DoesNotRequestASymbolAgainWhileItsFetchIsStillBeingWritten(t *testing.T) {
	decided := jstAt(10, 14, 10, 0, 0)
	rig, conn := newFollowupRig(t, decided)
	insertDecision(t, conn, 1, "trader", "LONG", decided)
	insertDecision(t, conn, 2, "trader", "SHORT", decided.Add(time.Minute)) // same symbol, later decision

	rig.gate.Lock() // the writer is slow
	rig.cycle(decided.Add(8*time.Minute), 0)
	if got := rig.inner.total(); got != 1 {
		t.Fatalf("requests = %d, want 1 (one symbol, however many decisions)", got)
	}
	rig.cycle(decided.Add(9*time.Minute), 0) // the next cycle, the write has not landed
	if got := rig.inner.total(); got != 1 {
		rig.gate.Unlock()
		t.Fatalf("requests = %d while the first fetch was still being written, want still 1", got)
	}
	rig.gate.Unlock()
	waitFor(t, rig.written)

	rig.evaluate(decided.Add(9*time.Minute + 30*time.Second))
	if r, ok := recordOf(t, conn, 1); !ok || r.verdict != "no_skip" {
		t.Fatalf("record = %+v, %v; want no_skip from the single followup fetch", r, ok)
	}
}

// The wait is bounded: a write that never lands (a full queue, a failed write)
// does not stop the symbol from being asked for again, once.
func TestFollowup_AskAgainWhenTheWriteNeverLanded(t *testing.T) {
	decided := jstAt(10, 14, 10, 0, 0)
	rig, conn := newFollowupRig(t, decided)
	insertDecision(t, conn, 1, "trader", "LONG", decided)

	rig.gate.Lock() // the write never lands during this test
	t.Cleanup(rig.gate.Unlock)
	rig.cycle(decided.Add(8*time.Minute), 0)
	rig.cycle(decided.Add(9*time.Minute), 0)
	if got := rig.inner.total(); got != 1 {
		t.Fatalf("requests = %d within the write lag, want 1", got)
	}
	rig.cycle(decided.Add(10*time.Minute+time.Second), 0) // the write lag has passed
	if got := rig.inner.total(); got != 2 {
		t.Fatalf("requests = %d after the write lag, want 2 (the lost write is asked for again)", got)
	}
}
