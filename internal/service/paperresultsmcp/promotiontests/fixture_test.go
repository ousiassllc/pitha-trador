package promotiontests

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// The fixture: 12 JST days (2026-10-01..12) of 26 trader decisions each, one instrument, the live
// prompt version and model. Every decision's bars are "up" (LONG +30 / SHORT -30 net bps at the default
// label) or "down" (LONG -30 / SHORT +30), so the live answer (always LONG) earns +30 or -30 and
// an answer that follows the market earns +30 on every row.
const (
	days        = 12
	perDay      = 26
	liveModel   = "live-model"
	strongModel = "strong-model"
)

var live = jev.TraderQuestionVersion

type decision struct {
	id   int
	day  int
	slot int
	up   bool
}

func (d decision) at() time.Time {
	return time.Date(2026, 10, d.day, 0, 0, 0, 0, time.UTC).Add(time.Duration(d.slot) * 20 * time.Minute)
}

func stamp(t *testing.T, ts time.Time) string {
	return testkit.Stored(t, ts.UTC().Format(time.RFC3339))
}

var (
	templateOnce sync.Once
	templateDir  string
	templateDB   string
	templateDecs []decision
)

// removeTemplate deletes the shared seeded database (TestMain).
func removeTemplate() {
	if templateDir != "" {
		_ = os.RemoveAll(templateDir)
	}
}

// seed gives w the fixture and returns its decisions. The 312 decisions and their 4056 bars are built once
// per test binary (the slow part under -race) into a template file; each test copies them into its own
// database with INSERT ... SELECT.
func seed(t *testing.T, w *sql.DB) []decision {
	t.Helper()
	templateOnce.Do(func() { buildTemplate(t) })
	if templateDB == "" {
		t.Fatal("the shared fixture could not be built")
	}
	ctx := t.Context()
	if _, err := w.ExecContext(ctx, `ATTACH DATABASE ? AS tpl`, templateDB); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"instruments", "jev_decisions", "market_snapshots"} {
		testkit.MustExec(t, w, `INSERT INTO main.`+table+` SELECT * FROM tpl.`+table)
	}
	testkit.MustExec(t, w, `DETACH DATABASE tpl`)
	return templateDecs
}

func buildTemplate(t *testing.T) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	templateDecs = writeFixture(t, w)
	if _, err := w.ExecContext(t.Context(), `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "promotiontests-")
	if err != nil {
		t.Fatal(err)
	}
	templateDir = dir
	file := filepath.Join(dir, "fixture.db")
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	templateDB = file
}

// writeFixture writes the instrument, the decisions and their bars (a fixed-seed draw, so the daily share of
// down rows differs from day to day: the day-cluster interval needs that spread).
func writeFixture(t *testing.T, w *sql.DB) []decision {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'a', 'TSE')`)
	rng := rand.New(rand.NewPCG(1091, 7))
	tx, err := w.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var out []decision
	id := 0
	for day := 1; day <= days; day++ {
		downShare := 0.3 + 0.4*rng.Float64()
		for slot := range perDay {
			id++
			d := decision{id: id, day: day, slot: slot, up: rng.Float64() >= downShare}
			out = append(out, d)
			ts := stamp(t, d.at())
			if _, err := tx.ExecContext(t.Context(), `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
				question_version, response_json, direction, confidence, latency_ms, model_id, request_cost, created_at)
				VALUES (?, 1, 'S', ?, 'trader', 'h', '{}', ?, '{}', 'LONG', 0.7, 5, ?, 0.004, ?)`, d.id, ts, live, liveModel, ts); err != nil {
				t.Fatal(err)
			}
			// the decision's 13 one-minute bars in one statement
			var sb strings.Builder
			args := make([]any, 0, 13*2)
			for i := range 13 {
				price := 1000.0
				switch {
				case i >= 2 && d.up:
					price = 1005
				case i >= 2:
					price = 995
				}
				if i > 0 {
					sb.WriteString(",")
				}
				sb.WriteString(`(1, 'S', ?, ?, ?, ?, 1, 1, 1, 0, '{}')`)
				args = append(args, stamp(t, d.at().Add(time.Duration(i)*time.Minute)), price, price, price)
			}
			if _, err := tx.ExecContext(t.Context(), `INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, bid, ask, volume, turnover, vwap,
				price_vs_vwap_bps, raw_data_json) VALUES `+sb.String(), args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return out
}

// shadowAnswers writes a shadow version's answers for the decisions it answers (answer returns "" to skip).
func shadowAnswers(t *testing.T, w *sql.DB, version, model string, cost float64, decs []decision, answer func(decision) string) {
	t.Helper()
	tx, err := w.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(t.Context(), `INSERT INTO jev_shadow_decisions (jev_decision_id, prompt_version, model_id, response_json, direction, regime, entry_quality,
		confidence, toxic_flow, liquidity_stressed, continuation_probability, latency_ms, input_tokens, request_cost, cost_is_estimate)
		VALUES (?, ?, ?, '{}', ?, 'TREND', 'good', 0.5, 0, 0, 0.5, 1, 100, ?, 0)`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stmt.Close() }()
	for _, d := range decs {
		if dir := answer(d); dir != "" {
			if _, err := stmt.ExecContext(t.Context(), d.id, version, model, dir, cost); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// follow answers with the side that wins on the decision's bars: +30 bps on every row.
func follow(d decision) string {
	if d.up {
		return "LONG"
	}
	return "SHORT"
}

func setting(t *testing.T, w *sql.DB, key, value string) { testkit.Setting(t, w, key, value) }

func confirmCutoff(t *testing.T, w *sql.DB) {
	t.Helper()
	setting(t, w, "metalabel.llm_cutoff", `"2026-09-01"`)
	setting(t, w, "metalabel.llm_cutoff_model", fmt.Sprintf("%q", liveModel))
}
