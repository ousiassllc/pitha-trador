//go:build !windows

package codexflow_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// The whole path with a mock CLI: trial.TraderOptions (the application's wiring)
// -> the Codex arm -> codexwire -> a shell script standing in for `codex`. No real
// CLI, login, network or key is involved.
const mockCodex = `#!/bin/sh
home="$CODEX_HOME"
[ -n "$home" ] || exit 99
if [ "$1" = login ]; then echo "Logged in using ChatGPT"; exit 0; fi
echo run >> "$home/runs"
env > "$home/seen_env"
cat > /dev/null
cat "$home/stdout"
exit "$(cat "$home/exit")"
`

// appTraderDB builds a Trader with trial.TraderOptions (the application's wiring)
// against a live Jev stub; the Codex CLI is the mock script of keys.Codex.
func appTraderDB(t *testing.T, keys trial.Keys) (*jev.Trader, *system.RuntimeSettingsRepository, func() int, int64, *judgement.TrialRepository, *sql.DB) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	live := httptest.NewServer(jevtest.TraderHandler(jev.TraderResponse{Direction: domain.JevDirectionNone, Confidence: 0.5}))
	t.Cleanup(live.Close)
	settings := system.NewRuntimeSettingsRepository(db)
	decisions := judgement.NewDecisionRepository(db)
	inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	client := jev.NewClient(jev.Config{BaseURL: live.URL, MaxAttempts: 1})
	opts := trial.TraderOptions(db, client, settings, nil, keys)
	tr := jev.NewTrader(client, decisions, rag.NewService(db, decisions, market.NewSnapshotRepository(db)), opts...)
	t.Cleanup(tr.Close)
	rows := func() int {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM jev_trial_decisions`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	return tr, settings, rows, inst.ID, judgement.NewTrialRepository(db), db
}

type mockHome struct{ bin, home string }

func newMockHome(t *testing.T) mockHome {
	t.Helper()
	dir := t.TempDir()
	m := mockHome{bin: filepath.Join(dir, "codex"), home: filepath.Join(dir, "home")}
	if err := os.WriteFile(m.bin, []byte(mockCodex), 0o755); err != nil { // #nosec G306 -- a test script that must be executable
		t.Fatal(err)
	}
	if err := os.Mkdir(m.home, 0o700); err != nil {
		t.Fatal(err)
	}
	return m
}

func (m mockHome) write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(m.home, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (m mockHome) runs(t *testing.T) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(m.home, "runs"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "run")
}

// answered makes the mock finish a turn with a valid answer to every question.
func (m mockHome) answered(t *testing.T) {
	t.Helper()
	questions, err := jev.TraderQuestionsFor("trader-v3")
	if err != nil {
		t.Fatal(err)
	}
	answers := map[string]any{}
	for id, q := range questions {
		if options, ok := q.Criteria.(systemone.Options); ok {
			answers[id] = map[string]any{"choice": options[0].Name, "confidence": 0.6}
		} else {
			answers[id] = 0.4
		}
	}
	msg, _ := json.Marshal(answers)
	item, _ := json.Marshal(map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": string(msg)}})
	m.write(t, "stdout", `{"type":"thread.started","thread_id":"th-e2e"}`+"\n"+string(item)+"\n"+
		`{"type":"turn.completed","usage":{"input_tokens":900,"output_tokens":60}}`+"\n")
	m.write(t, "exit", "0")
}

func TestAppWiring_CodexArmRunsTheMockCLIAndStoresASubscriptionRow(t *testing.T) {
	t.Setenv("CODEX_API_KEY", "must-not-reach-the-cli")
	t.Setenv("OPENAI_API_KEY", "must-not-reach-the-cli")
	m := newMockHome(t)
	m.answered(t)
	tr, settings, rows, instID, results, _ := appTraderDB(t, trial.Keys{Codex: trial.CodexConn{Bin: m.bin, Home: m.home}})
	arms, err := jevtrialsettings.EncodeArms([]jevtrialsettings.Arm{codexArm("cx")})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{jevtrialsettings.KeyArms: arms, jevtrialsettings.KeyEnabled: "1", jevtrialsettings.KeyCodexEnabled: "1"} {
		if err := settings.Set(context.Background(), k, v, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tr.Evaluate(context.Background(), instID, state(time.Now())); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	waitFor(t, "the Codex arm's row", func() bool { return rows() == 1 })

	d, err := results.Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "ok" || d.CostSource != "subscription" || d.CostUSD != nil || d.CountedCostUSD != 0 || d.Provider != "codex_chatgpt" || d.ServedProvider != "codex_chatgpt:codex-exec-v1" ||
		d.InputTokens == nil || *d.InputTokens != 900 || d.ResponseID != "th-e2e" {
		t.Errorf("row = %+v", d)
	}
	env, err := os.ReadFile(filepath.Join(m.home, "seen_env"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(env), "must-not-reach-the-cli") || strings.Contains(string(env), "API_KEY") {
		t.Error("an API key of the parent's environment reached the CLI")
	}
	raw, ok, err := settings.Get(context.Background(), jevtrialsettings.KeyCodexLogin)
	if err != nil || !ok || !strings.Contains(raw, `"chatgpt"`) {
		t.Errorf("recorded login = %q (%v, %v)", raw, ok, err)
	}
}

// A failed run (turn.failed with a usage-limit text, exit 1) stops the provider:
// the mock CLI is run once; the next decisions never start it again.
func TestAppWiring_CodexUsageLimitFromTheMockCLIStopsWithoutRetry(t *testing.T) {
	m := newMockHome(t)
	reset := time.Now().Add(5 * time.Hour).Truncate(time.Minute).Local()
	text := "You've hit your usage limit. Try again at " + reset.Format("Jan 2, 2006 3:04 PM") + "."
	failed, _ := json.Marshal(map[string]any{"type": "turn.failed", "error": map[string]any{"message": text}})
	m.write(t, "stdout", string(failed)+"\n")
	m.write(t, "exit", "1")
	tr, settings, rows, instID, results, db := appTraderDB(t, trial.Keys{Codex: trial.CodexConn{Bin: m.bin, Home: m.home}})
	arms, err := jevtrialsettings.EncodeArms([]jevtrialsettings.Arm{codexArm("cx")})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{jevtrialsettings.KeyArms: arms, jevtrialsettings.KeyEnabled: "1", jevtrialsettings.KeyCodexEnabled: "1"} {
		if err := settings.Set(context.Background(), k, v, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	skips := func() int {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM jev_trial_skips WHERE reason = 'stopped_usage_limit'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, err := tr.Evaluate(context.Background(), instID, state(time.Now())); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	waitFor(t, "the failed row", func() bool { return rows() == 1 })
	// The next two decisions are skipped as stopped; the CLI is not started again.
	for want := 1; want <= 2; want++ {
		if _, err := tr.Evaluate(context.Background(), instID, state(time.Now())); err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		waitFor(t, "a stopped_usage_limit skip", func() bool { return skips() == want })
	}
	d, err := results.Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "failed" || d.FailureCode != "usage_limit" || d.CostSource != "subscription" || strings.Contains(d.ErrorExcerpt, "Upgrade") {
		t.Errorf("row = %+v", d)
	}
	raw, _, _ := settings.Get(context.Background(), jevtrialsettings.KeyCodexHalt)
	h, err := jevtrialsettings.ParseCodexHalt(raw)
	if err != nil || h.Reason != "usage_limit" || !h.UntilKnown || h.Until != reset.UTC().Format(time.RFC3339) {
		t.Errorf("halt = %q (%v), want a usage_limit stop until %s", raw, err, reset.UTC().Format(time.RFC3339))
	}
	if n := m.runs(t); n != 1 {
		t.Errorf("the CLI ran %d times, want exactly 1 (no retry)", n)
	}
}
