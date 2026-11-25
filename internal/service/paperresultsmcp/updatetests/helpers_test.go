// Package updatetests holds the HTTP-level tests of the update tools (issue
// #1222): the MCP server, the application's mcpbridge and the real
// updater.Checker run over one migrated database, with GitHub, the SafeGate
// inputs and the application's quit all faked. Nothing downloads from, installs
// to or restarts a real application.
package updatetests

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/updatecontrol"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/service/updater/mcpbridge"
	"github.com/ousiassllc/pitha-trador/internal/version"
)

const testAPIKey = "TEST-API-KEY-do-not-leak-0123456789"

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

// The release signing pair of these tests; it signs nothing outside them.
var (
	signingKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	publicKey  = signingKey.Public().(ed25519.PublicKey)
)

const (
	installerName = "pitha-trador-windows-amd64-installer.exe"
	installerBody = "fake-installer-bytes-for-issue-1222-tests"
)

// github is a fake GitHub: the latest-release API plus the asset downloads.
type github struct {
	*httptest.Server
	tag        string
	badSig     bool // serve a signature of other bytes (a forged release)
	hits       atomic.Int64
	inFlight   atomic.Int64
	maxInFight atomic.Int64
	hold       time.Duration // each release lookup takes this long
}

func newGitHub(t *testing.T, tag string) *github {
	t.Helper()
	g := &github{tag: tag}
	checksums := hex.EncodeToString(sha256Sum(installerBody)) + "  " + installerName + "\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/ousiassllc/pitha-trador/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		g.hits.Add(1)
		n := g.inFlight.Add(1)
		defer g.inFlight.Add(-1)
		for old := g.maxInFight.Load(); n > old && !g.maxInFight.CompareAndSwap(old, n); old = g.maxInFight.Load() {
		}
		time.Sleep(g.hold)
		_ = json.NewEncoder(w).Encode(updater.Release{TagName: g.tag, Assets: []updater.Asset{
			{Name: installerName, BrowserDownloadURL: g.URL + "/download/installer.exe"},
			{Name: "checksums.txt", BrowserDownloadURL: g.URL + "/download/checksums.txt"},
			{Name: "checksums.txt.sig", BrowserDownloadURL: g.URL + "/download/checksums.txt.sig"},
		}})
	})
	mux.HandleFunc("/download/installer.exe", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(installerBody)) })
	mux.HandleFunc("/download/checksums.txt", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(checksums)) })
	mux.HandleFunc("/download/checksums.txt.sig", func(w http.ResponseWriter, _ *http.Request) {
		signed := checksums
		if g.badSig {
			signed = "not what was published"
		}
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(ed25519.Sign(signingKey, []byte(signed)))))
	})
	g.Server = httptest.NewServer(mux)
	t.Cleanup(g.Close)
	return g
}

func sha256Sum(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

// setVersion makes this build look like a release build of version v with the
// test public key embedded; the previous values come back with the test.
func setVersion(t *testing.T, v string) {
	t.Helper()
	oldVersion, oldKey := version.Version, version.ReleasePublicKey
	version.Version, version.ReleasePublicKey = v, base64.StdEncoding.EncodeToString(publicKey)
	t.Cleanup(func() { version.Version, version.ReleasePublicKey = oldVersion, oldKey })
}

// The SafeGate inputs: open positions are adjustable, the rest is calm.
type gateInputs struct{ openPositions atomic.Int64 }

func (g *gateInputs) OpenPositionCount(context.Context) (int, error) {
	return int(g.openPositions.Load()), nil
}

func (*gateInputs) State(context.Context) (domain.SystemState, []domain.KillSwitchEvent, error) {
	return domain.SystemStateRunning, nil, nil
}

func (*gateInputs) ListOrders(context.Context, string, int) ([]domain.PaperOrder, error) {
	return nil, nil
}

// quitter stands in for the application's QuitForUpdate: it only records the call.
type quitter struct {
	mu    sync.Mutex
	paths []string
}

func (q *quitter) QuitForUpdate(path string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.paths = append(q.paths, path)
}

func (q *quitter) calls() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.paths...)
}

// stack is the MCP server, the bridge and the real Checker over one database.
type stack struct {
	srv     *httptest.Server
	db      *sql.DB
	gh      *github
	gate    *gateInputs
	quit    *quitter
	checker *updater.Checker
	now     time.Time // the clock of the MCP server and the bridge
}

func newChecker(gh *github, gate *gateInputs) *updater.Checker {
	return updater.NewChecker(updater.Config{
		Owner: "ousiassllc", Repo: "pitha-trador",
		Gate:       updater.SafeGate{Positions: gate, State: gate, Orders: gate},
		HTTPClient: gh.Client(), BaseURL: gh.URL, DownloadURLPrefix: gh.URL + "/download/",
	})
}

// newStack serves the MCP over a migrated database. With bridge, the
// application's side runs too (published status, requests executed).
func newStack(t *testing.T, current, latest string, bridge bool) *stack {
	t.Helper()
	return newStackAt(t, current, latest, bridge, testkit.FixedNow())
}

// newStackAt is newStack with the clock of the MCP server and the bridge fixed
// at now (testkit.FixedNow is a Sunday, outside the 立会時間帯).
func newStackAt(t *testing.T, current, latest string, bridge bool, now time.Time) *stack {
	t.Helper()
	setVersion(t, current)
	path, db := testkit.NewMigratedDB(t) // before TMPDIR moves: the template database is built once, in the first test's temp dir
	tmp := t.TempDir()                   // the installer downloads land here and go with the test
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	st := &stack{db: db, gh: newGitHub(t, latest), gate: &gateInputs{}, quit: &quitter{}, now: now}
	st.checker = newChecker(st.gh, st.gate)

	cfg := settings.Default()
	cfg.Enabled, cfg.APIKey = true, testAPIKey
	srv := paperresultsmcp.New(paperresultsmcp.Options{
		Settings: cfg, DBPath: path, CapitalJPY: 30_000_000, Now: func() time.Time { return st.now },
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	st.srv = httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		st.srv.Close()
		_ = srv.Close()
	})
	if bridge {
		st.runBridge(t)
	}
	return st
}

func (st *stack) runBridge(t *testing.T) {
	t.Helper()
	b := mcpbridge.New(mcpbridge.Config{
		Store: updatecontrol.New(st.db), Checker: st.checker, Quitter: st.quit,
		Now: func() time.Time { return st.now }, Tick: 20 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.Run(ctx)
	}()
	// The application's first publication is what tells the MCP there is an updater.
	waitFor(t, func() bool {
		var n int
		return st.db.QueryRowContext(t.Context(), `SELECT count(*) FROM update_control_status`).Scan(&n) == nil && n == 1
	})
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

type response struct {
	status int
	body   []byte
}

func (st *stack) call(t *testing.T, name string, args any) response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, st.srv.URL, strings.NewReader(string(body)))
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

// data decodes a successful call's structuredContent.data into out.
func (r response) data(t *testing.T, tool string, out any) {
	t.Helper()
	var env struct {
		Result struct {
			StructuredContent struct {
				Tool string          `json:"tool"`
				Data json.RawMessage `json:"data"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil || r.status != http.StatusOK || env.Result.StructuredContent.Tool != tool {
		t.Fatalf("%s: status %d, body %s (%v)", tool, r.status, r.body, err)
	}
	if err := json.Unmarshal(env.Result.StructuredContent.Data, out); err != nil {
		t.Fatalf("%s: decode %s: %v", tool, env.Result.StructuredContent.Data, err)
	}
}

func (r response) errorCode(t *testing.T) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(r.body, &e); err != nil {
		t.Fatalf("decode error body %s: %v", r.body, err)
	}
	return e.Code
}

// applyRows are the update_apply requests recorded (the audit), oldest first.
type applyRow struct{ Actor, Client, State, Outcome, Reason string }

func (st *stack) applyRows(t *testing.T) []applyRow {
	t.Helper()
	rows, err := st.db.QueryContext(t.Context(), `SELECT actor, client, state, outcome, reason FROM update_control_requests WHERE kind = 'apply' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []applyRow
	for rows.Next() {
		var r applyRow
		if err := rows.Scan(&r.Actor, &r.Client, &r.State, &r.Outcome, &r.Reason); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// waitFor polls cond until it holds, failing the test after a bounded wait.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
