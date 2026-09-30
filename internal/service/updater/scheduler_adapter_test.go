package updater_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
)

type fakeQuitter struct {
	called        bool
	installerPath string
}

func (f *fakeQuitter) QuitForUpdate(installerPath string) {
	f.called = true
	f.installerPath = installerPath
}

func TestSchedulerAdapter_CheckForUpdate_TriggersQuitterOnceReady(t *testing.T) {
	setVersion(t, "v0.1.0")
	checksums := fmt.Sprintf("%s  pitha-trador-windows-amd64-installer.exe\n", installerChecksum(t))
	server := newGitHubMock(t, "v0.2.0", checksums)
	quitter := &fakeQuitter{}
	adapter := updater.SchedulerAdapter{Checker: newChecker(server, allowGate()), Quitter: quitter}

	if err := adapter.CheckForUpdate(context.Background()); err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if !quitter.called {
		t.Fatal("Quitter.QuitForUpdate was not called, want it called once a verified installer is ready")
	}
	if quitter.installerPath == "" {
		t.Fatal("Quitter.QuitForUpdate got an empty installer path")
	}
}

func TestSchedulerAdapter_CheckForUpdate_DoesNotQuitWhenNotReady(t *testing.T) {
	setVersion(t, "v0.1.0")
	checksums := fmt.Sprintf("%s  pitha-trador-windows-amd64-installer.exe\n", installerChecksum(t))
	server := newGitHubMock(t, "v0.1.0", checksums) // no newer release
	quitter := &fakeQuitter{}
	adapter := updater.SchedulerAdapter{Checker: newChecker(server, allowGate()), Quitter: quitter}

	if err := adapter.CheckForUpdate(context.Background()); err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if quitter.called {
		t.Fatal("Quitter.QuitForUpdate was called, want it untouched when already up to date")
	}
}

func TestSchedulerAdapter_CheckForUpdate_PropagatesErrorWithoutQuitting(t *testing.T) {
	setVersion(t, "v0.1.0")
	// Wrong checksum forces Checker.CheckForUpdate to return an error.
	checksums := "0000000000000000000000000000000000000000000000000000000000000000  pitha-trador-windows-amd64-installer.exe\n"
	server := newGitHubMock(t, "v0.2.0", checksums)
	quitter := &fakeQuitter{}
	adapter := updater.SchedulerAdapter{Checker: newChecker(server, allowGate()), Quitter: quitter}

	if err := adapter.CheckForUpdate(context.Background()); err == nil {
		t.Fatal("CheckForUpdate: err = nil, want the checksum-mismatch error to propagate")
	}
	if quitter.called {
		t.Fatal("Quitter.QuitForUpdate was called, want it untouched on a download/verify error")
	}
}

// TestSchedulerAdapter_UpdatePending_TrueOnlyWhileSafeGateHoldsNewerRelease
// regresses issue #240: a release held back by SafeGate returns a nil error
// from CheckForUpdate, so the scheduler learns it must retry soon only
// through UpdatePending; an up-to-date build must not report pending.
func TestSchedulerAdapter_UpdatePending_TrueOnlyWhileSafeGateHoldsNewerRelease(t *testing.T) {
	setVersion(t, "v0.1.0")
	checksums := fmt.Sprintf("%s  pitha-trador-windows-amd64-installer.exe\n", installerChecksum(t))
	held := updater.SafeGate{
		Positions: fakePositionCounter{count: 1},
		State:     fakeSystemStateReader{state: domain.SystemStateRunning},
		Orders:    fakeOrderLister{},
	}

	newer := newGitHubMock(t, "v0.2.0", checksums)
	adapter := updater.SchedulerAdapter{Checker: newChecker(newer, held), Quitter: &fakeQuitter{}}
	if err := adapter.CheckForUpdate(context.Background()); err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if !adapter.UpdatePending() {
		t.Fatal("UpdatePending = false, want true while the safety gate holds a newer release")
	}

	upToDate := newGitHubMock(t, "v0.1.0", checksums)
	adapter = updater.SchedulerAdapter{Checker: newChecker(upToDate, held), Quitter: &fakeQuitter{}}
	if err := adapter.CheckForUpdate(context.Background()); err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if adapter.UpdatePending() {
		t.Fatal("UpdatePending = true, want false when already up to date")
	}
}
