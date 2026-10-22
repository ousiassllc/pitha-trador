//go:build !windows

package sleepguard

import "log/slog"

// NewKeeper returns the Keeper of this OS. Only Windows is supported
// (issue #863); elsewhere Hold only says so once and the OS settings keep
// applying.
func NewKeeper() Keeper { return &unsupportedKeeper{} }

type unsupportedKeeper struct{ warned bool }

func (k *unsupportedKeeper) Hold() error {
	if !k.warned {
		k.warned = true
		slog.Warn("sleep guard: this OS is not supported (Windows only); the PC may still sleep")
	}
	return nil
}

func (*unsupportedKeeper) Release() error { return nil }
