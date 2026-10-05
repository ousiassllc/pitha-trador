package updater_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
)

// sequencedPositionCounter returns counts[i] on the i-th call (the last
// value repeats), modelling a position opening between the pre-download
// gate evaluation and the post-download one.
type sequencedPositionCounter struct {
	counts []int
	calls  int
}

func (s *sequencedPositionCounter) OpenPositionCount(context.Context) (int, error) {
	i := s.calls
	if i >= len(s.counts) {
		i = len(s.counts) - 1
	}
	s.calls++
	return s.counts[i], nil
}

func gateWith(positions updater.PositionCounter) updater.SafeGate {
	return updater.SafeGate{
		Positions: positions,
		State:     fakeSystemStateReader{state: domain.SystemStateRunning},
		Orders:    fakeOrderLister{},
	}
}

// TestSchedulerAdapter_CheckForUpdate_GateReevaluatedAfterDownload
// regresses issue #535: a gate condition that becomes true while the
// installer downloads must stop the quit/install and mark the check Blocked.
func TestSchedulerAdapter_CheckForUpdate_GateReevaluatedAfterDownload(t *testing.T) {
	setVersion(t, "v0.1.0")
	checksums := fmt.Sprintf("%s  %s\n", installerChecksum(t), installerName)
	server := newGitHubMock(t, "v0.2.0", checksums)

	positions := &sequencedPositionCounter{counts: []int{0, 1}} // opens during the download
	quitter := &fakeQuitter{}
	adapter := updater.SchedulerAdapter{Checker: newChecker(server, gateWith(positions)), Quitter: quitter}

	if err := adapter.CheckForUpdate(context.Background()); err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if quitter.called {
		t.Fatal("QuitForUpdate was called, want it skipped when the gate fails after the download")
	}
	status := adapter.Status()
	if !status.Blocked || status.BlockedKind != updater.BlockOpenPositions {
		t.Fatalf("Status = %+v, want Blocked with BlockOpenPositions", status)
	}
	if status.Ready {
		t.Fatalf("Status.Ready = true, want false when blocked after download")
	}
	if !adapter.UpdatePending() {
		t.Fatal("UpdatePending = false, want true so the scheduler retries soon")
	}
	if positions.calls != 2 {
		t.Fatalf("gate evaluated %d times, want 2 (before and after download)", positions.calls)
	}

	// The next tick, with the position closed again, installs.
	positions.counts = []int{0}
	if err := adapter.CheckForUpdate(context.Background()); err != nil {
		t.Fatalf("CheckForUpdate (retry): %v", err)
	}
	if !quitter.called {
		t.Fatal("QuitForUpdate was not called on the retry that passes both gate evaluations")
	}
}
