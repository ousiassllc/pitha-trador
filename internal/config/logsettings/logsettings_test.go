package logsettings_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/logsettings"
)

type fakeStore struct {
	rows map[string]string
	err  error
}

func (f fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f.rows[key]
	return v, ok, f.err
}

func TestParseLevel(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want slog.Level
	}{
		{`"debug"`, slog.LevelDebug}, {`"info"`, slog.LevelInfo}, {`"warn"`, slog.LevelWarn},
		{`"error"`, slog.LevelError}, {`" WARN "`, slog.LevelWarn},
	} {
		got, err := logsettings.ParseLevel(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseLevel(%s) = (%v, %v), want %v", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{`"trace"`, `""`, `3`, `info`, `null`} {
		if _, err := logsettings.ParseLevel(bad); err == nil {
			t.Errorf("ParseLevel(%s) accepted an invalid level", bad)
		}
	}
}

func TestParseRetentionDays(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{{`1`, 1}, {`30`, 30}, {`3650`, 3650}, {`7.0`, 7}} {
		got, err := logsettings.ParseRetentionDays(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseRetentionDays(%s) = (%d, %v), want %d", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{`0`, `-1`, `3651`, `1.5`, `"30"`, `null`, `abc`} {
		if _, err := logsettings.ParseRetentionDays(bad); err == nil {
			t.Errorf("ParseRetentionDays(%s) accepted an invalid value", bad)
		}
	}
}

func TestLoadLevel(t *testing.T) {
	ctx := context.Background()
	if got := logsettings.LoadLevel(ctx, fakeStore{}); got != logsettings.DefaultLevel {
		t.Errorf("unset: got %v, want default %v", got, logsettings.DefaultLevel)
	}
	store := fakeStore{rows: map[string]string{config.KeyLogLevel: `"warn"`}}
	if got := logsettings.LoadLevel(ctx, store); got != slog.LevelWarn {
		t.Errorf("stored warn: got %v", got)
	}
	bad := fakeStore{rows: map[string]string{config.KeyLogLevel: `"loud"`}}
	if got := logsettings.LoadLevel(ctx, bad); got != logsettings.DefaultLevel {
		t.Errorf("invalid row: got %v, want default", got)
	}
	failing := fakeStore{err: errors.New("db down")}
	if got := logsettings.LoadLevel(ctx, failing); got != logsettings.DefaultLevel {
		t.Errorf("store error: got %v, want default", got)
	}
}

func TestLoadRetentionDays(t *testing.T) {
	ctx := context.Background()
	if got := logsettings.LoadRetentionDays(ctx, fakeStore{}); got != logsettings.DefaultRetentionDays {
		t.Errorf("unset: got %d", got)
	}
	store := fakeStore{rows: map[string]string{config.KeyLogRetentionDays: `7`}}
	if got := logsettings.LoadRetentionDays(ctx, store); got != 7 {
		t.Errorf("stored 7: got %d", got)
	}
	bad := fakeStore{rows: map[string]string{config.KeyLogRetentionDays: `0`}}
	if got := logsettings.LoadRetentionDays(ctx, bad); got != logsettings.DefaultRetentionDays {
		t.Errorf("invalid row: got %d, want default", got)
	}
	failing := fakeStore{err: errors.New("db down")}
	if got := logsettings.LoadRetentionDays(ctx, failing); got != logsettings.DefaultRetentionDays {
		t.Errorf("store error: got %d, want default", got)
	}
}
