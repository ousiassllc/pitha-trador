package mcproute_test

import (
	"database/sql"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

// newExperimentsApp serves the Settings MCP card over a migrated database with
// the seeded closed positions (see testkit.SeedPositions) and the real gate.
func newExperimentsApp(t *testing.T) (*app, *sql.DB) {
	t.Helper()
	_, db := testkit.NewMigratedDB(t)
	testkit.SeedPositions(t, db)
	testkit.SeedRisk(t, db)
	return newAppWithGate(t, &fakeGate{enabled: true, ExperimentGate: mcpset.NewGate(db)}), db
}

func experimentForm(extra url.Values) url.Values {
	form := url.Values{
		"hypothesis": {"1 回の最大損失を 1% から 0.5% に下げる"}, "kind": {"setting"}, "name": {"risk.paper.max_trade_loss_pct"},
		"before": {"1"}, "after": {"0.5"},
	}
	for k, v := range extra {
		form[k] = v
	}
	return form
}

// The card loads the experiments section lazily; the section lists nothing at
// first, then shows a recorded experiment with its verdict, and the comparison
// shows the figures with the unapproved minimum stated.
func TestExperimentsSectionCreatesListsAndCompares(t *testing.T) {
	a, db := newExperimentsApp(t)

	if body := a.get("/settings/mcp-panel").Body.String(); !strings.Contains(body, `hx-get="/settings/mcp/experiments"`) {
		t.Errorf("the MCP card does not load the experiments section")
	}
	rec := a.get("/settings/mcp/experiments")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "mcp-experiments-empty") || !strings.Contains(body, `data-testid="mcp-experiment-form"`) ||
		!strings.Contains(body, "暫定の案で、まだ承認されていません") {
		t.Fatalf("empty section = %d: %s", rec.Code, body)
	}

	// A POST without the CSRF token stores nothing.
	if rec := a.post("/settings/mcp/experiments", experimentForm(nil), false); rec.Code == http.StatusOK {
		t.Fatalf("POST without CSRF = %d", rec.Code)
	}
	rec = a.post("/settings/mcp/experiments", experimentForm(url.Values{"started_at": {"2026-10-02T00:00"}}), true)
	body = rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "実験を記録しました") || !strings.Contains(body, `data-experiment-id="1"`) ||
		!strings.Contains(body, "risk.paper.max_trade_loss_pct") || !strings.Contains(body, "Settings 画面") ||
		!strings.Contains(body, "2026-10-02 00:00 〜 実行中") || !strings.Contains(body, `data-verdict="insufficient_trades"`) {
		t.Fatalf("create = %d: %s", rec.Code, body)
	}
	var actor string
	if err := db.QueryRow(`SELECT actor FROM tuning_experiments`).Scan(&actor); err != nil || actor != "gui" {
		t.Fatalf("stored actor = %q (%v), want gui", actor, err)
	}

	rec = a.get("/settings/mcp/experiments/1")
	body = rec.Body.String()
	for _, want := range []string{
		"実験 1 の前後比較", "1 回の最大損失を 1% から 0.5% に下げる", `data-verdict="insufficient_trades"`, "判定なし（取引件数が足りない）",
		"暫定の案で、まだ承認されていません", "純損益（コスト控除後）", "1 取引あたりの純損益（判定の基準）", "95% 信頼区間", "Welch の t 検定",
		"これより下げることはできません", "平均の勝ち", "平均の負け", "最大ドローダウン", "拒否の種類",
	} {
		if rec.Code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("comparison lacks %q (%d): %s", want, rec.Code, body)
		}
	}
	if rec := a.get("/settings/mcp/experiments/99"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown experiment = %d, want 404", rec.Code)
	}
	if rec := a.get("/settings/mcp/experiments/abc"); rec.Code != http.StatusNotFound {
		t.Errorf("bad id = %d, want 404", rec.Code)
	}
}

// The form carries one variable; the server refuses what the rules refuse and
// stores nothing.
func TestExperimentFormRefusesInvalidRequests(t *testing.T) {
	a, db := newExperimentsApp(t)
	for name, extra := range map[string]url.Values{
		"key off the whitelist": {"name": {"min_entry_quality"}},
		"unchanged value":       {"after": {"1"}},
		"empty hypothesis":      {"hypothesis": {" "}},
		"bad kind":              {"kind": {"weather"}},
		"bad start":             {"started_at": {"yesterday"}},
		"start in the future":   {"started_at": {"2099-01-01T00:00"}},
	} {
		rec := a.post("/settings/mcp/experiments", experimentForm(extra), true)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422 (%s)", name, rec.Code, rec.Body.String())
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tuning_experiments`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d experiments stored by invalid requests (%v)", n, err)
	}
}

// 終了 and 中止 end a running experiment once, behind the CSRF check; the GUI
// is not held by the MCP write switch.
func TestExperimentCloseAndAbortFromTheScreen(t *testing.T) {
	a, db := newExperimentsApp(t)
	a.post("/settings/mcp/experiments", experimentForm(nil), true)
	a.post("/settings/mcp/experiments", experimentForm(url.Values{"name": {"risk.max_daily_trades"}}), true)

	if rec := a.post("/settings/mcp/experiments/1/close", url.Values{"status": {"closed"}}, false); rec.Code == http.StatusOK {
		t.Fatalf("close without CSRF = %d", rec.Code)
	}
	rec := a.post("/settings/mcp/experiments/1/close", url.Values{"status": {"closed"}}, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "実験を終了しました") {
		t.Fatalf("close = %d: %s", rec.Code, rec.Body.String())
	}
	rec = a.post("/settings/mcp/experiments/2/close", url.Values{"status": {"aborted"}}, true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "実験を中止しました") || !strings.Contains(rec.Body.String(), `data-verdict="aborted"`) {
		t.Fatalf("abort = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := a.post("/settings/mcp/experiments/1/close", url.Values{"status": {"closed"}}, true); rec.Code != http.StatusConflict {
		t.Errorf("closing twice = %d, want 409", rec.Code)
	}
	if rec := a.post("/settings/mcp/experiments/99/close", url.Values{"status": {"closed"}}, true); rec.Code != http.StatusNotFound {
		t.Errorf("unknown experiment = %d, want 404", rec.Code)
	}
	if rec := a.post("/settings/mcp/experiments/1/close", url.Values{"status": {"paused"}}, true); rec.Code == http.StatusOK {
		t.Errorf("bad status accepted")
	}
	rows, err := db.Query(`SELECT status FROM tuning_experiments ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		got = append(got, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "closed,aborted" {
		t.Errorf("stored statuses = %v, want closed, aborted", got)
	}
}

// Without the gate the experiments routes are absent.
func TestExperimentRoutesNeedTheGate(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.session()
	if rec := a.get("/settings/mcp/experiments"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /settings/mcp/experiments without a gate = %d, want 404", rec.Code)
	}
	if rec := a.post("/settings/mcp/experiments", experimentForm(nil), true); rec.Code != http.StatusNotFound {
		t.Errorf("POST /settings/mcp/experiments without a gate = %d, want 404", rec.Code)
	}
}
