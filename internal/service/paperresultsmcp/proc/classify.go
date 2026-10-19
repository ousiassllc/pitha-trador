package proc

import (
	"strings"
	"sync"
	"time"
)

// FailureKind is why the MCP process is in the error state. It is a fixed
// vocabulary: neither the process's output nor an error string reaches the
// screen or the log.
type FailureKind string

const (
	// FailExeMissing: paper-results-mcp was not found beside the application.
	FailExeMissing FailureKind = "exe_missing"
	// FailSpawn: the operating system refused to start the process (or to
	// bind it to the application's lifetime, see platform_*.go).
	FailSpawn FailureKind = "spawn"
	// FailPortInUse: the process could not bind its address.
	FailPortInUse FailureKind = "port_in_use"
	// FailRefused: the process declined to listen (no auth configured, an
	// address outside the loopback/tailnet rule, disabled, ...).
	FailRefused FailureKind = "refused"
	// FailSettings: the process could not use the settings file.
	FailSettings FailureKind = "settings"
	// FailExited: the process ended without being asked to (a crash); the
	// exit code is the only detail.
	FailExited FailureKind = "exited"
	// FailNotListening: the process is alive but never opened its port.
	FailNotListening FailureKind = "not_listening"
)

// Failure is the last start failure or unexpected exit.
type Failure struct {
	Kind FailureKind
	// ExitCode is the process's exit code (-1: killed by a signal, or none).
	ExitCode int
	At       time.Time
}

// classify maps an unexpected exit to a Failure by looking for the process's
// own fixed log phrases in its stderr tail (cmd/paper-results-mcp and
// paperresultsmcp/server.go). The tail itself is dropped.
func classify(exit Exit, now time.Time) *Failure {
	kind := FailExited
	switch tail := exit.Stderr; {
	case strings.Contains(tail, "address already in use"), strings.Contains(tail, "Only one usage of each socket address"):
		kind = FailPortInUse
	case strings.Contains(tail, "settings file unusable"):
		kind = FailSettings
	case strings.Contains(tail, "refusing to listen"), strings.Contains(tail, "not listening"):
		kind = FailRefused
	}
	return &Failure{Kind: kind, ExitCode: exit.Code, At: now}
}

// tailBuffer keeps the last tailLimit bytes written to it: enough to
// classify an exit, too little to hold anything worth leaking.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

const tailLimit = 4 << 10

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailLimit {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-tailLimit:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
