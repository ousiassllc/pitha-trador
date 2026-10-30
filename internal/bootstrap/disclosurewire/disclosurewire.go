// Package disclosurewire wires the disclosure skip record (issue #935,
// FR-DISCLOSURE-SKIP-1〜5) into the composition root. It is split out of
// internal/bootstrap to keep that package within its size limit.
//
// The record only watches. It wraps the feed News Ingest already polls so each
// fetch is copied to a background writer, and a background evaluator later
// stores a verdict per Jev entry-candidate decision. Both use their own
// connection to the database (sidecar.Open), so that nothing the
// record does reaches the DB-write failure streak the Risk Engine reads, and
// neither runs on, or waits for, the trading path.
package disclosurewire

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb/sidecar"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosureskip"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

var errNotStarted = errors.New("disclosurewire: database connection not opened")

// evaluateInterval is how often the evaluator looks for decisions without a record.
const evaluateInterval = time.Minute

// Wire is the record's background part. The zero value (and nil) does nothing.
type Wire struct {
	dbPath string
	every  time.Duration // evaluateInterval; tests shorten it
	feed   *disclosureskip.RecordingFeed
	sink   *lazySink
}

// Wrap returns the feed News Ingest should poll and the Wire that runs its
// background part. The record is made for the default やのしん feed only (the
// index it is built on: newest-first, item-limited); with a custom feed
// (customFeed) or without a database path, feed is returned as it is and the
// Wire does nothing.
func Wrap(feed newsfeed.Feed, customFeed bool, dbPath string) (newsfeed.Feed, *Wire) {
	if customFeed || dbPath == "" {
		return feed, nil
	}
	sink := &lazySink{}
	recording := disclosureskip.NewRecordingFeed(feed, newsfeed.YanoshinLimit, sink)
	return recording, &Wire{dbPath: dbPath, every: evaluateInterval, feed: recording, sink: sink}
}

// Start opens the record's database connection and launches its goroutines on
// wg; they end with ctx. A connection that cannot be opened only switches the
// record off (logged): the queued fetches are dropped once the queue is full.
func (w *Wire) Start(ctx context.Context, wg *sync.WaitGroup) {
	if w == nil {
		return
	}
	conn, err := sidecar.Open(w.dbPath)
	if err != nil {
		slog.Warn("bootstrap: disclosure skip record disabled: cannot open its database connection", "error", err)
		return
	}
	store := disclosureskip.NewStore(conn)
	w.sink.set(store)
	evaluator := disclosureskip.NewEvaluator(store, marketcalendar.TSE)

	var running sync.WaitGroup
	running.Add(2)
	go func() { defer running.Done(); w.feed.Run(ctx) }()
	go func() { defer running.Done(); evaluator.Run(ctx, w.every) }()
	wg.Add(1)
	go func() { defer wg.Done(); running.Wait(); closeQuietly(conn) }()
	slog.Info("bootstrap: disclosure skip record enabled (record only; it never affects trading)")
}

func closeQuietly(conn *sql.DB) {
	if err := conn.Close(); err != nil {
		slog.Warn("bootstrap: closing the disclosure skip database connection failed", "error", err)
	}
}

// lazySink is the recording feed's sink before Start has opened the database:
// the writer only runs after set, so a write before then cannot happen; the
// error is the guard.
type lazySink struct {
	mu    sync.Mutex
	store *disclosureskip.Store
}

func (s *lazySink) set(store *disclosureskip.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = store
}

func (s *lazySink) WriteFetch(ctx context.Context, ev disclosureskip.FetchEvent) error {
	s.mu.Lock()
	store := s.store
	s.mu.Unlock()
	if store == nil {
		return errNotStarted
	}
	return store.WriteFetch(ctx, ev)
}
