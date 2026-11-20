package backtesttests

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab"
	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab/labtest"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

type listView struct {
	Runs       []runView `json:"runs"`
	NextCursor *string   `json:"next_cursor"`
}

func TestListIsNewestFirstWithStatusFilterAndCursor(t *testing.T) {
	srv := newServer(t, options{})
	for range 5 {
		srv.startAndWait(t, nil)
	}
	// Run 3 fails: plant the failure through the table (the checks of the row hold).
	execSQL(t, srv, `UPDATE backtest_runs SET status = 'failed', result_json = NULL, error_code = 'internal' WHERE id = 3`)

	var page listView
	srv.call(t, "backtest_list", map[string]any{"limit": 2}).data(t, "backtest_list", &page)
	if len(page.Runs) != 2 || page.Runs[0].RunID != 5 || page.Runs[1].RunID != 4 || page.NextCursor == nil {
		t.Fatalf("page 1 = %+v", page)
	}
	var next listView
	srv.call(t, "backtest_list", map[string]any{"limit": 2, "cursor": *page.NextCursor}).data(t, "backtest_list", &next)
	if len(next.Runs) != 2 || next.Runs[0].RunID != 3 || next.Runs[1].RunID != 2 || next.NextCursor == nil {
		t.Fatalf("page 2 = %+v", next)
	}
	var failed listView
	srv.call(t, "backtest_list", map[string]any{"status": "failed"}).data(t, "backtest_list", &failed)
	if len(failed.Runs) != 1 || failed.Runs[0].RunID != 3 || failed.Runs[0].ErrorCode == nil || *failed.Runs[0].ErrorCode != "internal" || failed.NextCursor != nil {
		t.Fatalf("failed filter = %+v", failed)
	}
	// The rows carry no result: that is backtest_get.
	if body := srv.call(t, "backtest_list", map[string]any{}).body; strings.Contains(string(body), `"variants"`) {
		t.Errorf("backtest_list returned results: %s", body)
	}

	srv.call(t, "backtest_list", map[string]any{"limit": 51}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
	srv.call(t, "backtest_list", map[string]any{"limit": 0}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "backtest_list", map[string]any{"status": "paused"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "backtest_list", map[string]any{"cursor": "nope"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "backtest_list", map[string]any{"from": "2026-10-01", "to": "2026-10-02"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
}

func TestGetOfAnUnknownOrBadRunID(t *testing.T) {
	srv := newServer(t, options{})
	srv.call(t, "backtest_get", map[string]any{"run_id": 42}).wantError(t, http.StatusNotFound, paperresultsmcp.CodeBacktestNotFound)
	srv.call(t, "backtest_get", map[string]any{}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "backtest_get", map[string]any{"run_id": 0}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "backtest_get", map[string]any{"run_id": "1"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
}

// A queued or running run is readable with what was asked for and no result yet.
func TestGetWhileRunningShowsTheRequestAndNoResult(t *testing.T) {
	block := labtest.NewBlocking()
	srv := newServer(t, options{source: func(context.Context, string) (backtestlab.ConfigSource, error) { return block, nil }})
	r := srv.start(t, map[string]any{"variants": twoVariants})
	<-block.Entered
	got := srv.get(t, r.RunID)
	if got.Status != "running" || got.StartedAt == nil || got.FinishedAt != nil || got.Variants != nil || got.ExploredCount != 2 || len(got.RequestedVariants) == 0 {
		t.Fatalf("get while running = %+v / %s", got.runView, got.RequestedVariants)
	}
	close(block.Release)
	srv.waitFinished(t, r.RunID)
}

// Only a fixed error code is ever shown: a failing source's text (with a path
// and a secret-looking value) reaches neither the tools nor the log.
func TestAFailureShowsOnlyAFixedCode(t *testing.T) {
	const leak = "SENTINEL_FACTORY_ERROR open /home/someone/secret.db: token=abc123"
	srv := newServer(t, options{source: func(context.Context, string) (backtestlab.ConfigSource, error) { return nil, errors.New(leak) }})
	r := srv.start(t, nil)
	srv.waitFinished(t, r.RunID)
	got := srv.get(t, r.RunID)
	if got.Status != "failed" || got.ErrorCode == nil || *got.ErrorCode != "unavailable" {
		t.Fatalf("get = %+v", got.runView)
	}
	for _, body := range [][]byte{srv.call(t, "backtest_get", map[string]any{"run_id": r.RunID}).body, srv.call(t, "backtest_list", map[string]any{}).body, srv.logs.Bytes()} {
		for _, secret := range []string{"SENTINEL_FACTORY_ERROR", "secret.db", "abc123", testAPIKey} {
			if strings.Contains(string(body), secret) {
				t.Errorf("%q leaked: %.200s", secret, body)
			}
		}
	}
	var stored string
	if err := srv.w.QueryRowContext(t.Context(), `SELECT error_code FROM backtest_runs WHERE id = ?`, r.RunID).Scan(&stored); err != nil || stored != "unavailable" {
		t.Errorf("stored error_code = %q (%v)", stored, err)
	}
}

// A process restart leaves running rows behind. Reading shows them as
// interrupted at once; the next start marks them failed in the table, and
// leaves this process's own unfinished run alone.
func TestRowsLeftRunningByAnEarlierProcessAreMarkedInterrupted(t *testing.T) {
	srv := newServer(t, options{owner: "this-process"})
	plant := func(status, owner string) {
		execSQL(t, srv, `INSERT INTO backtest_runs (status, owner, client, created_at, started_at, period_from, period_to, replay_mode, code_version, explored_count, request_json)
			VALUES (?, ?, 'c', '2026-10-17T00:00:00.000000000Z', CASE WHEN ? = 'queued' THEN NULL ELSE '2026-10-17T00:00:01.000000000Z' END, '2026-10-01', '2026-10-15', 'paper', 'dev', 1, '{"variants":[{"label":"baseline","params":{}}],"extra_cost_bps":[0]}')`,
			status, owner, status)
	}
	plant("running", "old-process")  // id 1
	plant("queued", "old-process")   // id 2
	plant("running", "this-process") // id 3: ours (cannot happen at start-up; the sweep must keep it)

	// Reading alone reports the old process's rows as failed / interrupted, without writing.
	for _, id := range []int64{1, 2} {
		got := srv.get(t, id)
		if got.Status != "failed" || got.ErrorCode == nil || *got.ErrorCode != "interrupted" || got.FinishedAt != nil {
			t.Errorf("run %d read as %+v", id, got.runView)
		}
	}
	if got := srv.get(t, 3); got.Status != "running" {
		t.Errorf("our own run read as %q", got.Status)
	}
	if n := count(t, srv.w, `SELECT COUNT(*) FROM backtest_runs WHERE status IN ('running', 'queued')`); n != 3 {
		t.Fatalf("reading changed the table: %d unfinished rows", n)
	}

	srv.startAndWait(t, nil) // the first start sweeps
	for id, want := range map[int64]string{1: "failed", 2: "failed", 3: "running", 4: "succeeded"} {
		var status, code, finished *string
		if err := srv.w.QueryRowContext(t.Context(), `SELECT status, error_code, finished_at FROM backtest_runs WHERE id = ?`, id).Scan(&status, &code, &finished); err != nil {
			t.Fatal(err)
		}
		if *status != want {
			t.Errorf("run %d stored as %q, want %q", id, *status, want)
		}
		if want == "failed" && (code == nil || *code != "interrupted" || finished == nil) {
			t.Errorf("run %d: error_code %v finished_at %v", id, code, finished)
		}
	}
}

// Stopping the server ends a run in flight as interrupted, before the writer closes.
func TestClosingTheServerEndsARunInFlightAsInterrupted(t *testing.T) {
	block := labtest.NewBlocking() // never released
	srv := newServer(t, options{source: func(context.Context, string) (backtestlab.ConfigSource, error) { return block, nil }})
	r := srv.start(t, nil)
	<-block.Entered
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	var status, code string
	if err := srv.w.QueryRowContext(t.Context(), `SELECT status, error_code FROM backtest_runs WHERE id = ?`, r.RunID).Scan(&status, &code); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || code != "interrupted" {
		t.Fatalf("stored %q / %q, want failed / interrupted", status, code)
	}
	srv.call(t, "backtest_start", withPeriod(nil)).wantError(t, http.StatusServiceUnavailable, paperresultsmcp.CodeBacktestUnavailable)
}

func execSQL(t *testing.T, srv *testServer, query string, args ...any) {
	t.Helper()
	if _, err := srv.w.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("seed: %v", err)
	}
}
