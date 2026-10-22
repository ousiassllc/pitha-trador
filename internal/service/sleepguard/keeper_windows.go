//go:build windows

package sleepguard

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
)

// SetThreadExecutionState flags (winbase.h).
const (
	esSystemRequired uintptr = 0x00000001
	esContinuous     uintptr = 0x80000000
)

var procSetThreadExecutionState = syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadExecutionState")

// NewKeeper returns the Windows Keeper: SetThreadExecutionState with
// ES_CONTINUOUS | ES_SYSTEM_REQUIRED while held (the display may still turn
// off), ES_CONTINUOUS alone to give it back.
//
// The execution state belongs to the calling OS thread and ends with it, so
// Hold starts a goroutine locked to its own thread, which sets the state and
// waits; Release makes it clear the state and exit (a goroutine that exits
// while locked takes its thread with it).
func NewKeeper() Keeper { return &windowsKeeper{} }

type windowsKeeper struct {
	mu   sync.Mutex
	stop chan struct{} // closed by Release; nil while not held
	done chan struct{} // closed when the locked goroutine has exited
}

func (k *windowsKeeper) Hold() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.stop != nil {
		return nil
	}
	stop, done, ready := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		runtime.LockOSThread() // never unlocked: the thread ends with this goroutine
		defer close(done)
		if err := setExecutionState(esContinuous | esSystemRequired); err != nil {
			ready <- err
			return
		}
		ready <- nil
		<-stop
		_ = setExecutionState(esContinuous)
	}()
	if err := <-ready; err != nil {
		<-done
		return err
	}
	k.stop, k.done = stop, done
	return nil
}

func (k *windowsKeeper) Release() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.stop == nil {
		return nil
	}
	close(k.stop)
	<-k.done
	k.stop, k.done = nil, nil
	return nil
}

// setExecutionState calls SetThreadExecutionState on the current thread; a
// zero return is the failure value.
func setExecutionState(flags uintptr) error {
	if r, _, err := procSetThreadExecutionState.Call(flags); r == 0 {
		return fmt.Errorf("SetThreadExecutionState(%#x): %w", flags, err)
	}
	return nil
}
