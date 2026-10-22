package shadow

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// cancelledByClose is true only when Close has cancelled the base context AND
// the error is a cancellation (also a wrapped one). A timeout
// (context.DeadlineExceeded) and any other error are real failures whatever
// the state of base.
func TestCancelledByClose(t *testing.T) {
	wrapped := fmt.Errorf("jev shadow: call: %w", context.Canceled)
	tests := []struct {
		name          string
		baseCancelled bool
		err           error
		want          bool
	}{
		{"live base, canceled", false, context.Canceled, false},
		{"live base, wrapped canceled", false, wrapped, false},
		{"live base, deadline exceeded", false, context.DeadlineExceeded, false},
		{"live base, plain error", false, errors.New("vendor said no"), false},
		{"cancelled base, canceled", true, context.Canceled, true},
		{"cancelled base, wrapped canceled", true, wrapped, true},
		{"cancelled base, deadline exceeded", true, context.DeadlineExceeded, false},
		{"cancelled base, plain error", true, errors.New("vendor said no"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.baseCancelled {
				cancel()
			}
			s := &Shadow{base: base, cancel: cancel}
			if got := s.cancelledByClose(tt.err); got != tt.want {
				t.Fatalf("cancelledByClose(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}
