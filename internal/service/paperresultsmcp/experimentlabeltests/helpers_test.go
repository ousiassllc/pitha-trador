// Package experimentlabeltests holds the HTTP-level tests of issue #1088: the
// label-based member of experiment_compare (it works with zero closed trades
// and never changes the trade-based fields), settings_set with a hypothesis
// (one experiment linked to the audit row, all or nothing), and
// experiments_list's unrecorded_changes. The pure statistics are tested in
// internal/service/paperresultsmcp/experiment/labelcompare.
package experimentlabeltests

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

type testServer struct {
	*paperresultsmcp.Server
	http *httptest.Server
	w    *sql.DB
}

type response struct {
	status int
	body   []byte
}

// newServer serves the MCP over a migrated database with one instrument and NO
// closed trade, with the fixed clock 2026-10-18 12:00 JST. w is a separate
// handle for planting rows and inspecting what was stored.
func newServer(t *testing.T) (*testServer, *sql.DB) { return newServerOn(t, "") }

func newServerOn(t *testing.T, driverName string) (*testServer, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'sentinel-free', 'TSE')`)
	testkit.SeedRisk(t, w)
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	srv := paperresultsmcp.New(paperresultsmcp.Options{
		Settings: s, DBPath: path, DriverName: driverName, Now: testkit.FixedNow, CapitalJPY: 30_000_000,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.Close()
		_ = srv.Close()
	})
	return &testServer{Server: srv, http: hs, w: w}, w
}

func (s *testServer) call(t *testing.T, name string, args any) response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.http.URL, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{status: res.StatusCode, body: b}
}

// raw returns the tool's data member undecoded.
func (r response) raw(t *testing.T, tool string) json.RawMessage {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("%s: status %d, body %s", tool, r.status, r.body)
	}
	var env struct {
		Result struct {
			StructuredContent struct {
				Tool string          `json:"tool"`
				Data json.RawMessage `json:"data"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil || env.Result.StructuredContent.Tool != tool {
		t.Fatalf("%s: bad envelope %s (%v)", tool, r.body, err)
	}
	return env.Result.StructuredContent.Data
}

func (r response) data(t *testing.T, tool string, out any) {
	t.Helper()
	if err := json.Unmarshal(r.raw(t, tool), out); err != nil {
		t.Fatalf("%s: decode data: %v", tool, err)
	}
}

// members decodes the data into its top-level members.
func (r response) members(t *testing.T, tool string) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	r.data(t, tool, &m)
	return m
}

func (r response) wantError(t *testing.T, status int, code paperresultsmcp.Code) {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(r.body, &e); err != nil || r.status != status || e.Code != string(code) {
		t.Fatalf("want %d %s, got %d %s (%s)", status, code, r.status, r.body, e.Code)
	}
}

func count(t *testing.T, w *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := w.QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func auditRow(t *testing.T, w *sql.DB, at, action, key, before, after string) int64 {
	t.Helper()
	res, err := w.ExecContext(t.Context(), `INSERT INTO mcp_settings_audit (at, actor, client, action, setting_key, before_value, after_value)
		VALUES (?, 'gui', '', ?, ?, ?, ?)`, testkit.Stored(t, at), action, key, before, after)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

var nextDecision = 1000

// decision plants a trader decision of direction at (UTC RFC3339) with the
// model / prompt version constant across the fixture, and, when net is not
// nil, its stored label (calibration_outcomes label_version 2, horizon 10).
func decision(t *testing.T, w *sql.DB, at, direction string, net *float64) {
	t.Helper()
	nextDecision++
	testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash,
		state_json, question_version, response_json, direction, confidence, latency_ms, model_id)
		VALUES (?, 1, '7203', ?, 'trader', 'h', '{}', 'q1', '{}', ?, 0.5, 5, 'm')`, nextDecision, testkit.Stored(t, at), direction)
	if net != nil {
		testkit.Outcome(t, w, nextDecision, 10, 2, nil, "timeout", *net)
	}
}

// researchDay marks a JST day as recorded with the mean net bps of 30m LONG
// rows (10 rows).
func researchDay(t *testing.T, w *sql.DB, day string, mean float64) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO research_label_days (day, snapshots, labeled_30m, labeled_60m, labeled_close, completed_at)
		VALUES (?, 10, 10, 0, 0, ?)`, day, testkit.Stored(t, "2026-10-18T00:00:00Z"))
	testkit.MustExec(t, w, `INSERT INTO research_label_day_stats (day, horizon, side, row_count, sum_gross, sum_net, sum_cost, wins,
		spread_rows, sum_spread, spread_above_cost, sum_gross_less_half_spread) VALUES (?, '30m', 'LONG', 10, ?, ?, 10, 5, 0, 0, 0, 0)`,
		day, mean*10+10, mean*10)
}

// The experiment of most tests: after = the JST days 10-12..10-16 (five days),
// before = the five days just before (10-07..10-11).
const (
	startedAt = "2026-10-11T15:00:00Z" // 2026-10-12 00:00 JST
	endedAt   = "2026-10-16T15:00:00Z" // 2026-10-17 00:00 JST
	key       = "risk.paper.max_trade_loss_pct"
)

var (
	beforeDates = []string{"2026-10-07", "2026-10-08", "2026-10-09", "2026-10-10", "2026-10-11"}
	afterDates  = []string{"2026-10-12", "2026-10-13", "2026-10-14", "2026-10-15", "2026-10-16"}
	jitter      = []float64{-0.4, -0.2, 0, 0.2, 0.4}
)

// plantLabels plants, for the first n days of before / after, two LONG
// decisions per day with the label (base + jitter), one SHORT and one NONE
// decision without label, and the research-label day sums (base + 2 + jitter).
func plantLabels(t *testing.T, w *sql.DB, n int, beforeBase, afterBase float64) {
	t.Helper()
	for _, plan := range []struct {
		dates []string
		base  float64
	}{{beforeDates, beforeBase}, {afterDates, afterBase}} {
		for i, d := range plan.dates[len(plan.dates)-n:] {
			v := plan.base + jitter[i]
			for range 2 {
				decision(t, w, d+"T01:00:00Z", "LONG", &v)
			}
			decision(t, w, d+"T02:00:00Z", "SHORT", nil)
			decision(t, w, d+"T03:00:00Z", "NONE", nil)
			researchDay(t, w, d, plan.base+2+jitter[i])
		}
	}
}

// plantedSwitch plants the audit row of the experiment's own change and returns its id.
func plantedSwitch(t *testing.T, w *sql.DB) int64 {
	t.Helper()
	return auditRow(t, w, startedAt, "set", key, "1", "0.5")
}

// createExperiment records the setting experiment of the fixture through the tool.
func createExperiment(t *testing.T, srv *testServer, auditID int64) experiment.Experiment {
	t.Helper()
	var e experiment.Experiment
	srv.call(t, "experiment_create", map[string]any{
		"hypothesis": "1 回の最大損失を 1% から 0.5% に下げる",
		"changes":    []map[string]any{{"kind": "setting", "name": key, "before": 1, "after": 0.5}},
		"started_at": startedAt, "audit_id": auditID,
	}).data(t, "experiment_create", &e)
	closeAt(t, srv, e.ID, endedAt)
	return e
}

func closeAt(t *testing.T, srv *testServer, id int64, ended string) {
	t.Helper()
	srv.call(t, "experiment_close", map[string]any{"experiment_id": id, "ended_at": ended}).data(t, "experiment_close", &experiment.Experiment{})
}

func srvNow() time.Time { return testkit.FixedNow() }

// signal plants a trade_signals row.
func signal(t *testing.T, w *sql.DB, id int64, at string, riskPassed bool, reason string) {
	t.Helper()
	testkit.TradeSignal(t, w, id, at, "policy-test", riskPassed, reason)
}
