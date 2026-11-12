package jevtrial_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	svc "github.com/ousiassllc/pitha-trador/internal/service/jevtrial"
)

// 2026-11-13 12:00 JST.
var now = time.Date(2026, 11, 13, 3, 0, 0, 0, time.UTC)

const validArms = `[{"id":"or-clef-flash","provider":"openrouter","model":"cloudflare/clef-flash","prompt_version":"trader-v3","daily_cost_cap_usd":0.02},{"id":"or-clef","provider":"openrouter","model":"cloudflare/clef","prompt_version":"trader-v3"}]`

type env struct {
	db       *sql.DB
	settings *system.RuntimeSettingsRepository
	s        *svc.Service
}

func newEnv(t *testing.T) env {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := svc.New(db)
	s.SetClock(func() time.Time { return now })
	return env{db: db, settings: system.NewRuntimeSettingsRepository(db), s: s}
}

func (e env) stored(t *testing.T, key string) (string, bool) {
	t.Helper()
	v, ok, err := e.settings.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return v, ok
}

func (e env) set(t *testing.T, key, value string) {
	t.Helper()
	if err := e.settings.Set(context.Background(), key, value, now); err != nil {
		t.Fatal(err)
	}
}

func TestState_DefaultIsOffWithNoArmsAndTheDefaultCap(t *testing.T) {
	st, err := newEnv(t).s.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled || len(st.Arms) != 0 || st.ArmsJSON != "[]" || !st.CapIsDefault || st.CapUSD != 0.10 || st.Halt != nil {
		t.Errorf("default state = %+v", st)
	}
	if st.TodayDate != "2026-11-13" || st.Today.Calls != 0 || st.Total.Calls != 0 || st.KeyKnown {
		t.Errorf("default state = %+v", st)
	}
}

func TestSave_PersistsTheExactKeysAndValues(t *testing.T) {
	e := newEnv(t)
	capUSD := 0.5
	if err := e.s.Save(context.Background(), svc.Form{Enabled: true, ArmsJSON: validArms, Cap: &capUSD}); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{jevtrialsettings.KeyEnabled: "1", jevtrialsettings.KeyDailyCostCapUSD: "0.5"} {
		if v, ok := e.stored(t, key); !ok || v != want {
			t.Errorf("%s = %q (%v), want %q", key, v, ok, want)
		}
	}
	raw, ok := e.stored(t, jevtrialsettings.KeyArms)
	arms, err := jevtrialsettings.ParseArms(raw)
	if !ok || err != nil || len(arms) != 2 || arms[0].ID != "or-clef-flash" || arms[0].DailyCostCapUSD != 0.02 {
		t.Fatalf("stored arms = %q (%v) %v", raw, ok, err)
	}
	st, err := e.s.State(context.Background())
	if err != nil || !st.Enabled || st.CapIsDefault || st.CapUSD != 0.5 || len(st.Arms) != 2 || st.Arms[1].CapUSD != jevtrialsettings.DefaultArmDailyCostCapUSD {
		t.Errorf("state after save = %+v, %v", st, err)
	}

	// Unchecked = off; a blank cap restores the default; blank arms = none.
	if err := e.s.Save(context.Background(), svc.Form{}); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.stored(t, jevtrialsettings.KeyEnabled); v != "0" {
		t.Errorf("switch = %q, want 0", v)
	}
	if _, ok := e.stored(t, jevtrialsettings.KeyDailyCostCapUSD); ok {
		t.Error("a blank cap did not restore the default")
	}
	if v, _ := e.stored(t, jevtrialsettings.KeyArms); v != "[]" {
		t.Errorf("arms = %q, want []", v)
	}
}

func TestSingleSetters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.s.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := e.s.SetCap(ctx, 0.25); err != nil {
		t.Fatal(err)
	}
	if err := e.s.SetArms(ctx, validArms); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.stored(t, jevtrialsettings.KeyEnabled); v != "1" {
		t.Errorf("enabled = %q", v)
	}
	if v, _ := e.stored(t, jevtrialsettings.KeyDailyCostCapUSD); v != "0.25" {
		t.Errorf("cap = %q", v)
	}
	if err := e.s.ResetCap(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.stored(t, jevtrialsettings.KeyDailyCostCapUSD); ok {
		t.Error("ResetCap kept the cap")
	}
	if err := e.s.SetEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.stored(t, jevtrialsettings.KeyEnabled); v != "0" {
		t.Errorf("enabled = %q, want 0 (disabling is always allowed)", v)
	}
}

func TestSave_RejectedInputSavesNothing(t *testing.T) {
	bad := func(v float64) *float64 { return &v }
	for name, form := range map[string]svc.Form{
		"arms not JSON":              {Enabled: true, ArmsJSON: `not json`},
		"arms not an array":          {Enabled: true, ArmsJSON: `{"id":"a"}`},
		"unknown field":              {Enabled: true, ArmsJSON: `[{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v3","modle":"x"}]`},
		"unregistered prompt":        {Enabled: true, ArmsJSON: `[{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v999"}]`},
		"duplicate arm":              {Enabled: true, ArmsJSON: `[{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v3"},{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v3"}]`},
		"cap zero (with valid arms)": {Enabled: true, ArmsJSON: validArms, Cap: bad(0)},
		"cap negative":               {Enabled: true, ArmsJSON: validArms, Cap: bad(-1)},
		"cap huge":                   {Enabled: true, ArmsJSON: validArms, Cap: bad(1e9)},
	} {
		e := newEnv(t)
		err := e.s.Save(context.Background(), form)
		if !errors.Is(err, svc.ErrInvalidInput) {
			t.Errorf("%s: error = %v, want ErrInvalidInput", name, err)
		}
		for _, key := range jevtrialsettings.Keys() {
			if _, ok := e.stored(t, key); ok {
				t.Errorf("%s: stored %s", name, key)
			}
		}
	}
}

func TestSetArms_UnregisteredPromptVersionIsNamed(t *testing.T) {
	err := newEnv(t).s.SetArms(context.Background(), `[{"id":"a","provider":"openrouter","model":"m","prompt_version":"trader-v999"}]`)
	if !errors.Is(err, svc.ErrInvalidInput) || !strings.Contains(err.Error(), "trader-v999") {
		t.Errorf("error = %v", err)
	}
}

var nextDecision int

// liveDecision seeds a live jev_decisions row (a trial row references one) and
// returns its id.
func liveDecision(t *testing.T, e env) int64 {
	t.Helper()
	ctx := context.Background()
	var instID int64
	if err := e.db.QueryRowContext(ctx, `SELECT id FROM instruments WHERE symbol = '7203'`).Scan(&instID); err != nil {
		res, err := e.db.ExecContext(ctx, `INSERT INTO instruments (symbol, name, market, is_active) VALUES ('7203', 'トヨタ自動車', 'TSE Prime', 1)`)
		if err != nil {
			t.Fatal(err)
		}
		if instID, err = res.LastInsertId(); err != nil {
			t.Fatal(err)
		}
	}
	nextDecision++
	live, err := judgement.NewDecisionRepository(e.db).Insert(ctx, domain.JevDecision{
		InstrumentID: instID, Symbol: "7203", Timestamp: now, DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "h" + strconv.Itoa(nextDecision), StateJSON: "{}", QuestionVersion: "trader-v3", ResponseJSON: "{}", ModelID: "jev-1.13.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	return live.ID
}

func insert(t *testing.T, e env, arm string, at time.Time, counted float64, status string) {
	t.Helper()
	str, f := func(v string) *string { return &v }, func(v float64) *float64 { return &v }
	cost := counted
	d := jevtrial.Decision{
		JevDecisionID: liveDecision(t, e), ArmID: arm, PromptVersion: "trader-v3", Provider: "openrouter", RequestedModel: "m",
		Status: status, CostUSD: &cost, CostSource: jevtrial.CostReported, CountedCostUSD: counted, CreatedAt: at,
	}
	if status == jevtrial.StatusOK {
		d.Direction, d.Regime, d.EntryQuality = str("LONG"), str("TREND"), str("good")
		d.ToxicFlow, d.LiquidityStressed, d.ContinuationProbability = f(0.1), f(0.2), f(0.6)
	} else {
		d.FailureCode = jevtrial.FailureHTTP5xx
	}
	if _, err := judgement.NewTrialRepository(e.db).Insert(context.Background(), d); err != nil {
		t.Fatal(err)
	}
}

func seedRows(t *testing.T, e env) {
	t.Helper()
	insert(t, e, "or-clef-flash", now.Add(-time.Hour), 0.01, jevtrial.StatusOK)
	insert(t, e, "or-clef-flash", now.Add(-2*time.Hour), 0.005, jevtrial.StatusFailed)
	insert(t, e, "or-clef", now.Add(-time.Hour), 0.03, jevtrial.StatusOK)
	insert(t, e, "or-clef", now.Add(-24*time.Hour), 0.07, jevtrial.StatusOK)
}

func TestState_ShowsPerArmNumbersAndHaltReasons(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.set(t, jevtrialsettings.KeyArms, validArms)
	seedRows(t, e)
	day := "2026-11-13"
	mustEncode := func(v string, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	e.set(t, jevtrialsettings.KeyHalt, mustEncode(jevtrialsettings.EncodeHalt(jevtrialsettings.KeyHalt, jevtrialsettings.Halt{Date: day, Reason: jevtrialsettings.HaltReasonPaymentRequired, Status: 402, At: now.Format(time.RFC3339)})))
	e.set(t, jevtrialsettings.HaltKey("or-clef-flash"), mustEncode(jevtrialsettings.EncodeHalt(jevtrialsettings.HaltKey("or-clef-flash"), jevtrialsettings.Halt{Date: day, Reason: jevtrialsettings.HaltReasonArmCostCap, SpentUSD: 0.015, CapUSD: 0.02, At: now.Format(time.RFC3339)})))
	e.set(t, jevtrialsettings.FailuresKey("or-clef-flash"), mustEncode(jevtrialsettings.EncodeFailures(jevtrialsettings.FailuresKey("or-clef-flash"), jevtrialsettings.Failures{Date: day, Count: 4})))
	until := now.Add(10 * time.Minute)
	e.set(t, jevtrialsettings.ThrottleKey("or-clef"), mustEncode(jevtrialsettings.EncodeThrottle(jevtrialsettings.ThrottleKey("or-clef"), jevtrialsettings.Throttle{Date: day, Count: 2, Status: 429, Reason: jevtrialsettings.ThrottleReason(429), At: now.Format(time.RFC3339), Until: until.Format(time.RFC3339)})))
	// A halt record of another day is not shown.
	e.set(t, jevtrialsettings.HaltKey("or-clef"), mustEncode(jevtrialsettings.EncodeHalt(jevtrialsettings.HaltKey("or-clef"), jevtrialsettings.Halt{Date: "2026-11-12", Reason: jevtrialsettings.HaltReasonForbidden, Status: 403, At: now.Add(-24 * time.Hour).Format(time.RFC3339)})))

	st, err := e.s.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Today.Calls != 3 || !approx(st.Today.CountedCostUSD, 0.045) || st.Total.Calls != 4 || !approx(st.Total.CountedCostUSD, 0.115) {
		t.Errorf("overall = today %+v total %+v", st.Today, st.Total)
	}
	if st.Halt == nil || st.Halt.Reason != jevtrialsettings.HaltReasonPaymentRequired {
		t.Errorf("whole-trial halt = %+v", st.Halt)
	}
	flash, clef := st.Arms[0], st.Arms[1]
	if flash.Arm.ID != "or-clef-flash" || flash.Today.Calls != 2 || !approx(flash.Today.CountedCostUSD, 0.015) || flash.CapUSD != 0.02 ||
		flash.FailuresToday != 4 || flash.FailureCap != jevtrialsettings.DailyFailureCap || flash.Halt == nil || flash.Halt.Reason != jevtrialsettings.HaltReasonArmCostCap {
		t.Errorf("flash = %+v", flash)
	}
	if clef.Today.Calls != 1 || !approx(clef.Today.CountedCostUSD, 0.03) || clef.Halt != nil || !clef.ThrottleActive || clef.ThrottlesToday != 2 || clef.Throttle.Status != 429 {
		t.Errorf("clef = %+v", clef)
	}
}

func approx(a, b float64) bool { d := a - b; return d < 1e-9 && d > -1e-9 }
