package replaywire_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/replaywire"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// seededDB is a migrated database with a pending replay run over one saved
// decision and one trial arm, as a started run would leave it.
func seededDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pitha.db")
	conn, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = conn.Close() }()
	for _, stmt := range []string{
		`INSERT INTO instruments (id, symbol, name, market, is_active) VALUES (1, '7203', 'x', 'TSE Prime', 1)`,
		`INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id)
			VALUES (1, 1, '7203', '2026-10-05T01:00:00.000000000Z', 'trader', 'h', '{"symbol":"7203"}', 'trader-v3', '{}', 1, 'm')`,
		`INSERT INTO runtime_settings (key, value, updated_at) VALUES ('jev.trial.arms',
			'[{"id":"a","provider":"openrouter","model":"vendor/a","prompt_version":"trader-v3"}]', '2026-11-15T00:00:00.000000000Z')`,
		`INSERT INTO jev_replay_runs (id, status, spec_json, period_from, period_to, max_rows, max_decision_id, arms_json,
			estimate_decisions, estimate_calls, estimate_max_cost_usd, estimate_token, created_at, updated_at)
			VALUES (1, 'pending', '{"decision_type":"trader","from":"2026-10-01","to":"2026-11-01","max_rows":10,"arms":["a"]}',
			'2026-09-30T15:00:00.000000000Z', '2026-10-31T15:00:00.000000000Z', 10, 1,
			'[{"id":"a","provider":"openrouter","model":"vendor/a","prompt_version":"trader-v3"}]', 1, 1, 1, 'tok',
			'2026-11-15T00:00:00.000000000Z', '2026-11-15T00:00:00.000000000Z')`,
	} {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	return path
}

// With the switch off (nothing stored) the wired runner makes no external
// request even with a key, an arm and a pending run, and it stops with the context.
func TestWire_DefaultOffMakesNoExternalRequestAndStopsWithTheContext(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	path := seededDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	replaywire.New(path, config.Secrets{OpenRouterAPIKey: "test-key-xxxx", OpenRouterBaseURL: server.URL}).Start(ctx, &wg)
	cancel()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the wire did not stop with its context")
	}

	if n := requests.Load(); n != 0 {
		t.Errorf("%d external requests with the replay switch off, want 0", n)
	}
}

// Without a database path, and for a nil Wire, Start does nothing.
func TestWire_NilAndPathlessWiresDoNothing(t *testing.T) {
	var wg sync.WaitGroup
	replaywire.New("", config.Secrets{}).Start(context.Background(), &wg)
	var none *replaywire.Wire
	none.Start(context.Background(), &wg)
	wg.Wait()
}
