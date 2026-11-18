package replaytests

import (
	"database/sql"
	"strconv"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const (
	armsJSON = `[` +
		`{"id":"known","provider":"openrouter","model":"vendor/known","prompt_version":"trader-v3","llm_cutoff":"2026-10-01"},` +
		`{"id":"unknown","provider":"openrouter","model":"vendor/unknown","prompt_version":"trader-v3"}]`
	// Four saved decisions: two on or before the known arm's cutoff day, two after it.
	decisionAtSep30 = "2026-09-30T01:00:00Z"
	decisionAtOct01 = "2026-10-01T23:00:00Z"
	decisionAtOct05 = "2026-10-05T01:00:00Z"
	decisionAtOct06 = "2026-10-06T01:00:00Z"
)

// seedDecisions plants four live trader decisions with a saved input, the arms
// of the trial settings and returns the decisions' ids in time order.
func seedDecisions(t *testing.T, w *sql.DB) []int64 {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market, is_active, created_at, updated_at) VALUES (1, '7203', 'x', 'TSE Prime', 1, 't', 't')`)
	testkit.MustExec(t, w, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('jev.trial.arms', ?, ?)`, armsJSON, testkit.Stored(t, "2026-10-10T00:00:00Z"))
	var ids []int64
	for i, ts := range []string{decisionAtSep30, decisionAtOct01, decisionAtOct05, decisionAtOct06} {
		id := int64(i + 1)
		dir := "LONG"
		if i%2 == 1 {
			dir = "SHORT"
		}
		testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, direction, latency_ms, model_id)
			VALUES (?, 1, '7203', ?, 'trader', 'h', ?, 'trader-v3', '{}', ?, 1, 'm')`,
			id, testkit.Stored(t, ts), `{"symbol":"7203","price":`+strconv.Itoa(1000+i)+`}`, dir)
		ids = append(ids, id)
	}
	return ids
}

func switchOn(t *testing.T, w *sql.DB) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('jev.replay.enabled', '1', ?)`, testkit.Stored(t, "2026-10-10T00:00:00Z"))
}

// plantRun inserts a run directly (as the application would have started it).
func plantRun(t *testing.T, w *sql.DB, id int, status string) {
	t.Helper()
	finished := any(nil)
	if status == "completed" || status == "stopped" || status == "failed" {
		finished = testkit.Stored(t, "2026-10-10T01:00:00Z")
	}
	testkit.MustExec(t, w, `INSERT INTO jev_replay_runs (id, status, spec_json, period_from, period_to, max_rows, max_decision_id, arms_json,
		estimate_decisions, estimate_calls, estimate_max_cost_usd, estimate_token, created_at, finished_at, updated_at)
		VALUES (?, ?, '{"decision_type":"trader","from":"2026-09-19","to":"2026-10-19","max_rows":10,"arms":["known","unknown"]}', ?, ?, 10, 4, ?, 4, 6, 0.01, 'tok', ?, ?, ?)`,
		id, status, testkit.Stored(t, "2026-09-18T15:00:00Z"), testkit.Stored(t, "2026-10-18T15:00:00Z"), armsJSON,
		testkit.Stored(t, "2026-10-10T00:00:00Z"), finished, testkit.Stored(t, "2026-10-10T00:00:00Z"))
}

// plantResult inserts one replay result row.
func plantResult(t *testing.T, w *sql.DB, runID int, decisionID int64, arm, status, direction, cutoff string) {
	t.Helper()
	reference := 0
	if cutoff == "unverified" {
		reference = 1
	}
	failure, dir, regime, quality, noul := "", any(direction), any("TREND"), any("good"), any(0.1)
	cost := any(0.00002)
	if status == "failed" {
		failure, dir, regime, quality, noul = "http_5xx", nil, nil, nil, nil
		cost = nil
	}
	source := "reported"
	if cost == nil {
		source = "unknown"
	}
	testkit.MustExec(t, w, `INSERT INTO jev_replay_results (run_id, jev_decision_id, arm_id, prompt_version, requested_model, status, failure_code, error_excerpt,
		direction, regime, entry_quality, toxic_flow, liquidity_stressed, continuation_probability, response_json, cost_usd, cost_source, counted_cost_usd,
		llm_cutoff_status, reference_only, created_at)
		VALUES (?, ?, ?, 'trader-v3', 'm', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0.00002, ?, ?, ?)`,
		runID, decisionID, arm, status, failure, sentinelError, dir, regime, quality, noul, noul, noul, sentinelBody, cost, source,
		cutoff, reference, testkit.Stored(t, "2026-10-10T00:30:00Z"))
}
