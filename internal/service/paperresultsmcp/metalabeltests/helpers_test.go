// Package metalabeltests holds the HTTP-level tests of the meta-labeling
// diagnosis tools metalabel_training_readiness and jev_models (issues #848,
// #849). The first is checked against the nightly retraining's own Trainer on
// the same rows; the second against the cutoff gate's exact model comparison.
// They live apart from mcptests, which holds the other read tools' tests.
package metalabeltests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
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

type testServer struct {
	*paperresultsmcp.Server
	http *httptest.Server
}

type response struct {
	status int
	body   []byte
}

// newTestServer serves Handler on httptest; now is the tool's clock.
func newTestServer(t *testing.T, dbPath string, now time.Time) *testServer {
	t.Helper()
	return newTestServerWith(t, paperresultsmcp.Options{DBPath: dbPath}, now)
}

// newTestServerWith is newTestServer with more of the options set.
func newTestServerWith(t *testing.T, opts paperresultsmcp.Options, now time.Time) *testServer {
	t.Helper()
	s := settings.Default()
	s.Enabled = true
	s.APIKey = testAPIKey
	opts.Settings, opts.CapitalJPY, opts.Now = s, 30_000_000, func() time.Time { return now }
	opts.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := paperresultsmcp.New(opts)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.Close()
		_ = srv.Close()
	})
	return &testServer{Server: srv, http: hs}
}

func (s *testServer) call(t *testing.T, name string, args any) response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
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
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{status: res.StatusCode, body: b}
}

// data decodes a successful tool response's structuredContent.data into out
// and logs it, so `go test -v` shows the JSON the tool really returned.
func (r response) data(t *testing.T, tool string, out any) {
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
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("%s: decode %s: %v", tool, r.body, err)
	}
	if env.Result.StructuredContent.Tool != tool || env.Result.IsError {
		t.Fatalf("%s: bad envelope %s", tool, r.body)
	}
	t.Logf("%s -> %s", tool, env.Result.StructuredContent.Data)
	if err := json.Unmarshal(env.Result.StructuredContent.Data, out); err != nil {
		t.Fatalf("%s: decode data %s: %v", tool, env.Result.StructuredContent.Data, err)
	}
}

// decision is one seeded trader decision with its triple-barrier label.
type decision struct {
	id        int
	at        time.Time
	direction string
	model     string
	state     string // state_json
	response  string // response_json
	ofi       *float64
	micro     *float64
	horizon   int
	net       any // net_return_bps (nil: the label has no net return)
	label     int // label_version of the outcome row; 0: no outcome row
}

var (
	validState    = `{"spread_bps":3.2,"return_1m":0.0004}`
	validResponse = `{"direction":"LONG","entry_quality":"good","toxic_flow":0.1,"liquidity_stressed":0.1,"continuation_probability":0.6,"confidence":0.7}`
)

// insertDecision writes d into jev_decisions and, when d.label != 0, a
// calibration_outcomes row, the way the application stores them.
func insertDecision(t *testing.T, w *sql.DB, d decision) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash,
		state_json, question_version, response_json, direction, confidence, latency_ms, model_id, ofi_short, ofi_mid, ofi_long,
		microprice_dev_bps) VALUES (?, 1, '7203', ?, 'trader', 'h', ?, 'q1', ?, ?, 0.7, 5, ?, ?, ?, ?, ?)`,
		d.id, testkit.StoredAt(t, d.at), d.state, d.response, d.direction, d.model, d.ofi, d.ofi, d.ofi, d.micro)
	if d.label != 0 {
		testkit.MustExec(t, w, `INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return,
			max_adverse_excursion, max_favorable_excursion, was_direction_correct, barrier_hit, net_return_bps,
			entry_ref_price, label_version) VALUES (?, ?, 0, 0, 0, 1, 'tp', ?, 100, ?)`, d.id, d.horizon, d.net, d.label)
	}
}

// learnable returns n complete, strongly learnable decisions (the signed order
// flow predicts the net return), one every 20 minutes, the last one at end,
// ids from firstID.
func learnable(n, firstID int, end time.Time, model string) []decision {
	rng := rand.New(rand.NewPCG(11, 7))
	out := make([]decision, n)
	for i := range out {
		direction, sign := domain.JevDirectionLong, 1.0
		if i%2 == 1 {
			direction, sign = domain.JevDirectionShort, -1.0
		}
		ofi := rng.NormFloat64()
		net := -20.0
		if rng.Float64() < 1/(1+math.Exp(-2*ofi)) {
			net = 30
		}
		micro, flow := sign*ofi, sign*ofi
		out[i] = decision{
			id: firstID + i, at: end.Add(-time.Duration(n-1-i) * 20 * time.Minute),
			direction: direction, model: model, ofi: &flow, micro: &micro, horizon: 15, net: net, label: 2,
			state: fmt.Sprintf(`{"spread_bps":%.2f,"return_1m":%.5f}`, 3+rng.Float64(), 0.001*rng.NormFloat64()),
			response: fmt.Sprintf(`{"direction":%q,"entry_quality":"good","toxic_flow":%.2f,"liquidity_stressed":0.1,"continuation_probability":0.6,"confidence":%.2f}`,
				direction, 0.3*rng.Float64(), 0.6+0.2*rng.Float64()),
		}
	}
	return out
}

// seedInstrument creates instrument 1 the decisions refer to.
func seedInstrument(t *testing.T, w *sql.DB) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`)
}
