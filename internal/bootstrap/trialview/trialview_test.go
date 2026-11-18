package trialview_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/trialview"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/trialquery"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/trialresults"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

// seed plants, an hour or more before now: three live trader decisions and arm-a's rows for them
// (1: ok LONG, reported cost; 2: ok NONE, unknown cost; 3: failed, unknown cost), a counterfactual
// label on decision 2 (arm-a's NONE answer), and a skip of arm-b (an arm without a row) on decision 3.
func seed(t *testing.T, w *sql.DB, now time.Time) {
	t.Helper()
	dirs := []string{"LONG", "LONG", "SHORT"}
	for i, dir := range dirs {
		id := i + 1
		at := now.Add(-time.Duration(4-id) * time.Hour).UTC()
		testkit.MustExec(t, w, `INSERT INTO jev_decisions (id, instrument_id, symbol, timestamp, decision_type, state_hash, state_json,
			question_version, response_json, direction, confidence, latency_ms, model_id)
			VALUES (?, 1, '7203', ?, 'trader', 'h', '{}', 'trader-v3', '{}', ?, 0.7, 5, 'm')`, id, testkit.StoredAt(t, at), dir)
	}
	repo := judgement.NewTrialRepository(w)
	cost := 0.0123
	insert := func(d jevtrial.Decision) {
		t.Helper()
		d.ArmID, d.PromptVersion, d.Provider, d.RequestedModel, d.LLMCutoffStatus = "arm-a", "trader-v3", "openrouter", "vendor/arm-a", jevtrial.CutoffUnverified
		if d.Status == "" {
			d.Status = jevtrial.StatusOK
		}
		if d.Status == jevtrial.StatusOK {
			eq, regime := "fair", "TREND"
			d.EntryQuality, d.Regime = &eq, &regime
			zero, half := 0.1, 0.5
			d.ToxicFlow, d.LiquidityStressed, d.ContinuationProbability = &zero, &zero, &half
		}
		if _, err := repo.Insert(t.Context(), d); err != nil {
			t.Fatal(err)
		}
	}
	long, none := "LONG", "NONE"
	insert(jevtrial.Decision{JevDecisionID: 1, Direction: &long, CostUSD: &cost, CostSource: jevtrial.CostReported, CountedCostUSD: cost})
	insert(jevtrial.Decision{JevDecisionID: 2, Direction: &none, CostSource: jevtrial.CostUnknown, CountedCostUSD: 0.5})
	insert(jevtrial.Decision{JevDecisionID: 3, Status: jevtrial.StatusFailed, FailureCode: "timeout", CostSource: jevtrial.CostUnknown})
	testkit.MustExec(t, w, `INSERT INTO counterfactual_labels (jev_decision_id, horizon_minutes, barrier_hit, net_return_bps,
		entry_ref_price, label_version, future_return, max_adverse_excursion, max_favorable_excursion, llm_cutoff, created_at)
		VALUES (2, 5, 'sl', -12.5, 100, 2, 0, 0, 0, '2025-01-01', ?)`, testkit.StoredAt(t, now))
	if err := repo.InsertSkip(t.Context(), jevtrial.Skip{JevDecisionID: 3, ArmID: "arm-b", Reason: jevtrial.SkipKeyNotSet, CreatedAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
}

// The screen's source returns exactly what the MCP tools' aggregation returns for the same period.
func TestReader_ReturnsTheMCPToolsAggregation(t *testing.T) {
	now := time.Now()
	_, w := testkit.NewMigratedDB(t)
	seed(t, w, now)
	rng := query.Range{From: now.Add(-48 * time.Hour), To: now.Add(time.Hour)}
	r := trialview.Reader{DB: w}

	got, err := r.Compare(t.Context(), rng)
	if err != nil {
		t.Fatal(err)
	}
	want, err := trialquery.Compare(t.Context(), w, rng, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || got.Cohort.Rows.Total != 3 {
		t.Errorf("Compare differs from trialquery.Compare (or no rows): %+v", got.Cohort)
	}
	gotCost, err := r.Cost(t.Context(), rng, now)
	if err != nil {
		t.Fatal(err)
	}
	wantCost, err := trialquery.Cost(t.Context(), w, rng, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotCost, wantCost) || gotCost.Total.Unknown.Calls != 2 || gotCost.Total.Unknown.ActualUSD != nil {
		t.Errorf("Cost differs from trialquery.Cost: %+v", gotCost.Total)
	}
}

// The whole path (database -> aggregation -> page): the page shows the figures the tools return, and
// an unknown cost stays a count.
func TestPage_OverTheDatabaseShowsTheToolsFigures(t *testing.T) {
	now := time.Now()
	_, w := testkit.NewMigratedDB(t)
	seed(t, w, now)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	trialresults.New(trialview.Reader{DB: w}).Register(engine)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/performance/trial?days=7", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, body)
	}
	wantMissing := fmt.Sprintf("%.1f%%（1 / 3）", 100.0/3)
	for _, want := range []string{
		wantMissing,             // arm-a: 1 failed of 3 rows
		"$0.0123（1 件）",          // reported
		"不明（2 件）",               // two calls without a known cost
		"API キーが未設定だった",         // arm-b's skip, an arm without a row
		`data-arm="arm-b"`,      // shown, with no missing rate
		"NONE と反実仮想（参考値・proxy）", // proxy block
		"-12.5 bps",             // the existing label of arm-a's NONE answer
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	// The unknown calls' counted amount (0.5, a cap-counting upper bound) is not shown as a cost.
	if strings.Contains(body, "$0.5") || strings.Contains(body, "0.5000") || strings.Contains(body, `data-testid="trial-empty"`) {
		t.Errorf("unknown cost shown as an amount, or empty state with records: %s", body)
	}
}

// With no trial record the page is the empty state.
func TestPage_OverAnEmptyDatabaseIsTheEmptyState(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	trialresults.New(trialview.Reader{DB: w}).Register(engine)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/performance/trial", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-testid="trial-empty"`) {
		t.Errorf("status %d: %s", rec.Code, rec.Body.String())
	}
}
