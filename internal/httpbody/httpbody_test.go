package httpbody_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/httpbody"
)

func TestReadAll(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"under limit", 9, false},
		{"exactly at limit", 10, false},
		{"one byte over limit", 11, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := httpbody.ReadAll(strings.NewReader(strings.Repeat("x", tt.size)), 10)
			if tt.wantErr {
				if !errors.Is(err, httpbody.ErrTooLarge) {
					t.Fatalf("err = %v, want ErrTooLarge", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if len(got) != tt.size {
				t.Fatalf("len = %d, want %d", len(got), tt.size)
			}
		})
	}
}
