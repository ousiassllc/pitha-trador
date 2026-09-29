package updater_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
)

func TestChecker_Status_ZeroBeforeAnyCheck(t *testing.T) {
	server := newGitHubMock(t, "v0.2.0", "")
	if status := newChecker(server, allowGate()).Status(); !status.CheckedAt.IsZero() || status.Available {
		t.Fatalf("Status before any check = %+v, want zero value", status)
	}
}

func TestChecker_Status_UpToDate(t *testing.T) {
	setVersion(t, "v0.1.1")
	server := newGitHubMock(t, "v0.1.1", "")
	checker := newChecker(server, allowGate())

	if _, err := checker.CheckForUpdate(context.Background()); err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	status := checker.Status()
	if status.CheckedAt.IsZero() || status.Available || status.Blocked || status.LastError != "" {
		t.Fatalf("Status = %+v, want checked and nothing available", status)
	}
}

func TestChecker_Status_DevBuild(t *testing.T) {
	setVersion(t, "dev")
	server := newGitHubMock(t, "v0.2.0", "")
	checker := newChecker(server, allowGate())

	if _, err := checker.CheckForUpdate(context.Background()); err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	status := checker.Status()
	if !status.DevBuild || status.Available || status.CheckedAt.IsZero() {
		t.Fatalf("Status = %+v, want DevBuild without Available", status)
	}
}

func TestChecker_Status_NewerReleaseBlockedBySafeGate(t *testing.T) {
	setVersion(t, "v0.1.0")
	server := newGitHubMock(t, "v0.2.0", "")
	gate := updater.SafeGate{
		Positions: fakePositionCounter{count: 1}, // open position: gate rejects
		State:     fakeSystemStateReader{state: domain.SystemStateRunning},
		Orders:    fakeOrderLister{},
	}
	checker := newChecker(server, gate)

	result, err := checker.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	if result.Ready {
		t.Fatal("Ready = true, want false while the safety gate rejects")
	}
	status := checker.Status()
	if !status.Available || status.Version != "v0.2.0" || !status.Blocked || status.Ready {
		t.Fatalf("Status = %+v, want Available v0.2.0, Blocked, not Ready", status)
	}
}

func TestChecker_Status_NewerReleaseReady(t *testing.T) {
	setVersion(t, "v0.1.0")
	checksums := fmt.Sprintf("%s  pitha-trador-windows-amd64-installer.exe\n", installerChecksum(t))
	server := newGitHubMock(t, "v0.2.0", checksums)
	checker := newChecker(server, allowGate())

	if _, err := checker.CheckForUpdate(context.Background()); err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	status := checker.Status()
	if !status.Available || status.Version != "v0.2.0" || status.Blocked || !status.Ready {
		t.Fatalf("Status = %+v, want Available v0.2.0, Ready, not Blocked", status)
	}
}

// A failed check must not forget that a newer release was already seen:
// the failure says nothing about whether it still exists.
func TestChecker_Status_ErrorKeepsPreviousAvailability(t *testing.T) {
	setVersion(t, "v0.1.0")
	server := newGitHubMock(t, "v0.2.0", "")
	gate := updater.SafeGate{
		Positions: fakePositionCounter{count: 1},
		State:     fakeSystemStateReader{state: domain.SystemStateRunning},
		Orders:    fakeOrderLister{},
	}
	checker := newChecker(server, gate)
	if _, err := checker.CheckForUpdate(context.Background()); err != nil {
		t.Fatalf("first CheckForUpdate: %v", err)
	}

	server.Close() // next check cannot reach the release API
	if _, err := checker.CheckForUpdate(context.Background()); err == nil {
		t.Fatal("second CheckForUpdate err = nil, want a network error")
	}
	status := checker.Status()
	if status.LastError == "" || !status.Available || status.Version != "v0.2.0" || !status.Blocked {
		t.Fatalf("Status = %+v, want LastError set and Available/Blocked kept", status)
	}
}
