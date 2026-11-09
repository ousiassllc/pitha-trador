// Package historytests holds the HTTP-level tests of metalabel_history's
// adoption diagnostics (issue #1045): numeric_checks, first_unmet, criteria,
// comparison, gate and forward of each model. They are checked against the
// retraining's own training.AcceptanceConfig.Judge and the Calibration API's
// meta_model, on synthetic databases only.
package historytests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

// period covers every model the tests store (trained from 2026-10-05).
var period = map[string]any{"from": "2026-10-01", "to": "2026-10-17"}

type testServer struct {
	*paperresultsmcp.Server
	http *httptest.Server
}

// newServer serves Handler on httptest; the tool's clock is testkit.FixedNow.
func newServer(t *testing.T, dbPath string) *testServer {
	t.Helper()
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	now := testkit.FixedNow()
	srv := paperresultsmcp.New(paperresultsmcp.Options{
		DBPath: dbPath, Settings: s, CapitalJPY: 30_000_000, Now: func() time.Time { return now },
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.Close()
		_ = srv.Close()
	})
	return &testServer{Server: srv, http: hs}
}

// history calls metalabel_history and returns the raw structuredContent.data
// bytes and its decoded form.
func (s *testServer) history(t *testing.T, extra map[string]any) ([]byte, map[string]any) {
	t.Helper()
	args := map[string]any{}
	for k, v := range period {
		args[k] = v
	}
	for k, v := range extra {
		args[k] = v
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "metalabel_history", "arguments": args}})
	req, err := http.NewRequest(http.MethodPost, s.http.URL, bytes.NewReader(body))
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
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Result struct {
			StructuredContent struct {
				Data json.RawMessage `json:"data"`
			} `json:"structuredContent"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || res.StatusCode != http.StatusOK || env.Result.IsError {
		t.Fatalf("metalabel_history: status %d, err %v, body %s", res.StatusCode, err, raw)
	}
	var data map[string]any
	if err := json.Unmarshal(env.Result.StructuredContent.Data, &data); err != nil {
		t.Fatalf("decode data %s: %v", env.Result.StructuredContent.Data, err)
	}
	return raw, data
}

// models returns the response's models by version.
func (s *testServer) models(t *testing.T) map[string]map[string]any {
	t.Helper()
	_, data := s.history(t, nil)
	list, _ := data["models"].([]any)
	out := make(map[string]map[string]any, len(list))
	for _, m := range list {
		m := m.(map[string]any)
		out[m["version"].(string)] = m
	}
	return out
}

// row is one metalabel_models row as the tests store it.
type row struct {
	version, status, reason string
	trained                 time.Time
	samples, takes, days    int
	evCI, aucCI             *repo.Interval
	// adoption and forward are the stored JSON: a string is stored as is (to
	// seed an unreadable one), anything else is marshalled.
	adoption, forward any
}

func jsonText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func nullable(iv *repo.Interval) (low, high any) {
	if iv == nil {
		return nil, nil
	}
	return iv.Low, iv.High
}

// insert writes r the way the Trainer stores a model.
func insert(t *testing.T, w *sql.DB, r row) {
	t.Helper()
	if r.adoption == nil {
		r.adoption = "{}"
	}
	if r.forward == nil {
		r.forward = "{}"
	}
	at := testkit.StoredAt(t, r.trained)
	evLow, evHigh := nullable(r.evCI)
	aucLow, aucHigh := nullable(r.aucCI)
	testkit.MustExec(t, w, `INSERT INTO metalabel_models (version, algorithm, label_version, status, reject_reason,
		trained_at, train_start, train_end, oos_start, oos_end, train_samples, oos_samples,
		feature_names_json, params_json, oos_auc, oos_expected_value_bps, oos_calibration_json,
		oos_takes, oos_days, oos_ev_ci_low_bps, oos_ev_ci_high_bps, oos_auc_ci_low, oos_auc_ci_high,
		adoption_json, forward_json, created_at)
		VALUES (?, 'logreg', 2, ?, ?, ?, ?, ?, ?, ?, 400, ?, '[]', '{"secret":"PARAMS-SENTINEL"}', 0.6, 3.5, '[]',
		?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.version, r.status, r.reason, at, at, at, at, at, r.samples,
		r.takes, r.days, evLow, evHigh, aucLow, aucHigh, jsonText(r.adoption), jsonText(r.forward), at)
}

// minute is a trained_at inside the period, n minutes apart so the order is fixed.
func minute(n int) time.Time { return time.Date(2026, 10, 5, 0, n, 0, 0, time.UTC) }

// bar is the adoption bar the tests judge against (not the current default).
func bar() repo.Criteria {
	return repo.Criteria{MinOOSSamples: 100, MinTakes: 50, MinOOSDays: 7, MinEVLowerBps: 0, MinAUCLower: 0.5, Alpha: 0.01, Resamples: 1000, Seed: 42}
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
