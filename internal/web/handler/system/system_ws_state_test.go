package system_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// sequencedSystemEngine returns states in order, then repeats the last one.
type sequencedSystemEngine struct {
	states []domain.SystemState
	calls  atomic.Int64
}

func (f *sequencedSystemEngine) State(context.Context) (domain.SystemState, []domain.KillSwitchEvent, error) {
	i := int(f.calls.Add(1)) - 1
	if i >= len(f.states) {
		i = len(f.states) - 1
	}
	return f.states[i], nil, nil
}
func (f *sequencedSystemEngine) Pause(context.Context) error  { return nil }
func (f *sequencedSystemEngine) Resume(context.Context) error { return nil }
func (f *sequencedSystemEngine) Kill(context.Context) error   { return nil }

// Issue #363: leaving Killed (AutoResume / another window's Resume) and
// Running↔Paused from another window must be pushed as state_changed, and
// entering Killed must remain a single kill_switch message.
func TestSystemHandler_WebSocket_PushesStateChangedOnTransitions(t *testing.T) {
	engine := &sequencedSystemEngine{states: []domain.SystemState{
		domain.SystemStateKilled,
		domain.SystemStateKilled,
		domain.SystemStateRunning,
		domain.SystemStateRunning,
		domain.SystemStatePaused,
		domain.SystemStateKilled,
	}}
	ctx, conn := dialSystemWS(t, engine)

	want := []string{
		`{"type":"kill_switch","reason":"manual"}`,
		`{"type":"state_changed","state":"running"}`,
		`{"type":"state_changed","state":"paused"}`,
		`{"type":"kill_switch","reason":"manual"}`,
	}
	for i, w := range want {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("message %d: conn.Read() error = %v, want %s", i, err, w)
		}
		if string(data) != w {
			t.Fatalf("message %d = %s, want %s", i, data, w)
		}
	}
}
