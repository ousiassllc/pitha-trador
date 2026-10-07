package fullscanflow_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

// lockedBuffer is a bytes.Buffer safe for the scheduler's goroutines to log to.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startLogged starts a scheduler built with opts and returns what it logged
// during Start. It swaps the process-wide default logger, so callers must not
// run in parallel.
func startLogged(t *testing.T, opts ...scheduler.Option) string {
	t.Helper()
	db := newTestDB(t)
	s := scheduler.New(jobqueue.NewJobRepository(db), market.NewInstrumentRepository(db), opts...)

	var out lockedBuffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := s.Start(ctx, time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Stop()
	return out.String()
}

// scan.full_scan_enabled: true (issues #693/#694/#696): startup warns that
// the ~8 minute bar spacing leaves the history-based features missing.
func TestScheduler_Start_FullScanOnWarnsAboutHistoryFeatures(t *testing.T) {
	logged := startLogged(t)
	for _, want := range []string{"level=WARN", "full scan on (scan.full_scan_enabled: true)", "about every 8 minutes", "ranking watch"} {
		if !strings.Contains(logged, want) {
			t.Errorf("startup log lacks %q:\n%s", want, logged)
		}
	}
}

// The default (full scan off) does not warn.
func TestScheduler_Start_FullScanOffDoesNotWarn(t *testing.T) {
	logged := startLogged(t, scheduler.WithFullScanDisabled())
	if strings.Contains(logged, "full scan on") || strings.Contains(logged, "level=WARN") {
		t.Errorf("startup log has a full-scan warning although the full scan is off:\n%s", logged)
	}
}
