package apierror_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/apierror"
)

func TestKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"500", &jev.APIError{StatusCode: 500}, apierror.HTTP5xx},
		{"529 overloaded", &jev.APIError{StatusCode: 529}, apierror.HTTP5xx},
		{"429", &jev.APIError{StatusCode: 429}, apierror.HTTP429},
		{"401", &jev.APIError{StatusCode: 401}, apierror.HTTP4xxOther},
		{"wrapped status", fmt.Errorf("trader: %w", &jev.APIError{StatusCode: 503}), apierror.HTTP5xx},
		{"deadline", fmt.Errorf("call: %w", context.DeadlineExceeded), apierror.Timeout},
		{"net timeout", &net.DNSError{IsTimeout: true}, apierror.Timeout},
		{"refused", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, apierror.Network},
		{"invalid response", fmt.Errorf("x: %w", systemone.ErrInvalidResponse), apierror.InvalidResponse},
		{"persist", fmt.Errorf("x: %w", jev.ErrPersistDecision), apierror.PersistFailed},
		{"cancelled", context.Canceled, apierror.Other},
		{"unrecognised", errors.New("boom"), apierror.Other},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := apierror.Kind(tc.err); got != tc.want {
				t.Errorf("Kind(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
