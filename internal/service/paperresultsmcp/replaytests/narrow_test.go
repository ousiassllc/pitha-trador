package replaytests

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/replaytool"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// The replay tools' write path is narrow: across a whole session the only
// statements that change anything are the INSERT of a pending run and the UPDATE
// that sets a run's stop flag, both on jev_replay_runs. The replay switch, the
// trial's settings, the live decisions and the results are never written, and
// no tool reads the secrets.
func TestReplayWritePathIsNarrow(t *testing.T) {
	driverName, counter := testkit.NewCountingDriver(t)
	srv, w := newServerOn(t, driverName)
	seedDecisions(t, w)
	switchOn(t, w)

	var est replaytool.EstimateView
	srv.call(t, "replay_estimate", map[string]any{"max_rows": 10}).data(t, "replay_estimate", &est)
	var run replaytool.RunView
	srv.call(t, "replay_start", map[string]any{"max_rows": 10, "confirm_token": est.ConfirmToken}).data(t, "replay_start", &run)
	srv.call(t, "replay_start", map[string]any{"max_rows": 10, "confirm_token": est.ConfirmToken}).wantError(t, 422, "invalid_value") // a second active run
	srv.call(t, "replay_progress", map[string]any{"run_id": run.RunID}).data(t, "replay_progress", &replaytool.ProgressList{})
	srv.call(t, "replay_results", map[string]any{"run_id": run.RunID}).data(t, "replay_results", &replaytool.ResultsView{})
	srv.call(t, "replay_stop", map[string]any{"run_id": run.RunID}).data(t, "replay_stop", &replaytool.RunView{})

	allowed := regexp.MustCompile(`(?is)^\s*(INSERT INTO jev_replay_runs\b|UPDATE jev_replay_runs SET stop_requested = 1, updated_at = \? WHERE id = \? AND status IN \('pending', 'running'\))`)
	written := 0
	for _, stmt := range counter.StatementLog() {
		s := strings.ToUpper(strings.TrimSpace(stmt))
		changes := strings.HasPrefix(s, "INSERT") || strings.HasPrefix(s, "UPDATE") || strings.HasPrefix(s, "DELETE") ||
			strings.HasPrefix(s, "CREATE") || strings.HasPrefix(s, "DROP") || strings.HasPrefix(s, "ALTER")
		if !changes {
			if strings.Contains(s, "SECRETS") || strings.Contains(s, "SELECT *") {
				t.Errorf("a replay tool read outside its tables: %s", testkit.FirstLine(stmt))
			}
			continue
		}
		if !allowed.MatchString(stmt) {
			t.Errorf("a write statement outside the replay run table: %s", testkit.FirstLine(stmt))
			continue
		}
		written++
	}
	if written != 2 {
		t.Errorf("%d write statements, want one INSERT (the start) and one UPDATE (the stop)", written)
	}
	if n := count(t, w, "jev_replay_results"); n != 0 {
		t.Errorf("%d results: the MCP never records an answer", n)
	}
}
