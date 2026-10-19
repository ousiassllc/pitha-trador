package jobfailurelog_test

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/jobfailurelog"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// lockedBuffer lets the Run goroutine's logs be read after it returns.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

// signalingSource signals after every read, so a test knows a tick finished.
type signalingSource struct {
	jobfailurelog.Source
	read chan struct{}
}

func (s *signalingSource) ListFailedAfter(ctx context.Context, c jobqueue.FailedCursor, upTo time.Time, limit int) ([]jobqueue.FailedJob, error) {
	jobs, err := s.Source.ListFailedAfter(ctx, c, upTo, limit)
	s.read <- struct{}{}
	return jobs, err
}

// Run reports once per received tick and stops with its context; the real
// repository's rows are read end to end through it.
func TestRun_ReportsPerTickFromTheRealRepositoryAndStopsOnCancel(t *testing.T) {
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	repo := jobqueue.NewJobRepository(conn)
	ctx := context.Background()
	for i := range 3 {
		at := t0.Add(time.Duration(i+1) * time.Second)
		job, err := repo.Enqueue(ctx, jobqueue.JobQueueMarketData, `{"payload":"board"}`, at)
		if err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
		if err := repo.MarkFailed(ctx, job.ID, at, kabuErr); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}
	}

	sink := &lockedBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(sink, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	runCtx, cancel := context.WithCancel(ctx)
	ticks := make(chan time.Time)
	done := make(chan struct{})
	src := &signalingSource{Source: repo, read: make(chan struct{}, 8)}
	rep := jobfailurelog.New(src, t0)
	go func() { defer close(done); rep.Run(runCtx, ticks) }()

	ticks <- t0.Add(time.Minute)
	ticks <- t0.Add(2 * time.Minute)
	// Each tick reads once (one short page); cancel only after both did.
	for range 2 {
		select {
		case <-src.read:
		case <-time.After(time.Minute):
			t.Fatal("tick was not handled")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not stop after the context was cancelled")
	}

	sink.mu.Lock()
	got := records(t, bytes.NewBufferString(sink.b.String()))
	sink.mu.Unlock()
	if len(got) != 1 || got[0].str("error_code") != "kabu_4001007" || got[0].num("count") != 3 || got[0].str("queues") != "market-data=3" {
		t.Fatalf("records = %v, want one kabu_4001007 x3 record (second tick finds nothing new)", got)
	}
}
