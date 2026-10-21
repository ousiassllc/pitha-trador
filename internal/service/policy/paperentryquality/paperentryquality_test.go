package paperentryquality_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/paperpolicysettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/paperentryquality"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

// inner is the engine's policy source before the Paper overlay: the shipped
// Live thresholds (min_entry_quality strong) and a fixed applied version.
type inner struct{ applied string }

func (i inner) CurrentThresholds(context.Context) (config.PolicyConfig, error) {
	return testThresholds().Policy, nil
}
func (i inner) AppliedPolicyVersion(context.Context) (string, error) { return i.applied, nil }

// source is the Paper minimum entry quality as the Settings screen / MCP would
// change it between two decisions; reads counts how often a decision looked at it.
type source struct {
	s     paperpolicysettings.Settings
	err   error
	reads int
}

func (p *source) PaperEntryQuality(context.Context) (paperpolicysettings.Settings, error) {
	p.reads++
	return p.s, p.err
}

func testThresholds() policy.Thresholds {
	dir := config.PolicyDirectionThresholds{
		MinProbability: 0.68, MinEntryQuality: "strong",
		MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
	}
	return policy.Thresholds{Policy: config.PolicyConfig{Long: dir, Short: dir}, MaxSpreadBps: 50, MinTurnover5mJPY: 3_000_000}
}

func ptr[T any](v T) *T { return &v }

// paperEQEnv is a migrated test database with one instrument; candidate stores
// a Jev decision (response_json carries the entry quality, as in production)
// and returns the Policy Engine input for it.
type paperEQEnv struct {
	db      *sql.DB
	signals *trading.SignalRepository
	instrID int64
	decRepo *judgement.DecisionRepository
}

func newPaperEQEnv(t *testing.T) paperEQEnv {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	instrument, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create instrument: %v", err)
	}
	return paperEQEnv{db: db, signals: trading.NewSignalRepository(db), instrID: instrument.ID, decRepo: judgement.NewDecisionRepository(db)}
}

// candidate is a Jev answer that clears every threshold except possibly
// entry_quality (continuation 0.60, toxic_flow 0.35, liquidity_stressed 0.25 -
// the boundary values - and a spread well inside the limit).
func (e paperEQEnv) candidate(t *testing.T, direction, entryQuality string) policy.Input {
	t.Helper()
	d := domain.JevDecision{
		Direction: ptr(direction), EntryQuality: ptr(entryQuality), Confidence: ptr(0.68),
		ContinuationProbability: ptr(0.60), ToxicFlow: ptr(0.35), LiquidityStressed: ptr(0.25),
	}
	ts := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	saved, err := e.decRepo.Insert(context.Background(), domain.JevDecision{
		InstrumentID: e.instrID, Symbol: "7203", Timestamp: ts, DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "hash", StateJSON: "{}", QuestionVersion: "v1", ModelID: "test-model",
		ResponseJSON: fmt.Sprintf(`{"direction":%q,"entry_quality":%q}`, direction, entryQuality),
		Direction:    d.Direction, EntryQuality: d.EntryQuality, Confidence: d.Confidence,
	})
	if err != nil {
		t.Fatalf("Insert decision: %v", err)
	}
	d.ID = saved.ID
	return policytest.WithInputs(policy.Input{
		InstrumentID: e.instrID, Symbol: "7203", Timestamp: ts, Decision: &d,
		SpreadBps: ptr(10.0), EntryPriceReference: ptr(2110.5), Calibrated: true,
	})
}

// engine is the real Policy Engine over the test DB whose policy source is the
// Paper overlay of mode over the shipped Live thresholds, with a meta-labeling
// model that takes every candidate and a Risk Engine that passes.
func (e paperEQEnv) engine(mode metalabelsettings.Mode, src paperentryquality.Source) *policy.Engine {
	return policy.NewEngine(testThresholds(), nil, e.signals,
		policy.WithPolicySource(paperentryquality.Policy{Inner: inner{}, Mode: mode, Source: src}),
		policytest.Option(policytest.ModelOf(policytest.TakingModel()), &policytest.Predictions{}))
}

func reject(sig domain.TradeSignal) string {
	if sig.RejectReason == nil {
		return ""
	}
	return *sig.RejectReason
}

// Paper: good (with every other threshold cleared) is no longer stopped by the
// entry quality and goes on to the meta-labeling model and the Risk Engine;
// fair is still rejected, now with min="good". The Paper value is read at
// every decision, so a change applies to the next one.
func TestPolicy_PaperUsesPaperMinEntryQuality(t *testing.T) {
	for _, direction := range []string{domain.JevDirectionLong, domain.JevDirectionShort} {
		t.Run(direction, func(t *testing.T) {
			env := newPaperEQEnv(t)
			src := &source{s: paperpolicysettings.Defaults()}
			e := env.engine(metalabelsettings.ModePaper, src)
			ctx := context.Background()

			good, err := e.Evaluate(ctx, env.candidate(t, direction, domain.JevEntryQualityGood))
			if err != nil {
				t.Fatalf("Evaluate good: %v", err)
			}
			if good.Direction != direction || !good.RiskPassed || good.RejectReason != nil {
				t.Errorf("Paper + good = %+v (%q), want %s entering (no entry_quality rejection; the model takes it)", good, reject(good), direction)
			}

			fair, err := e.Evaluate(ctx, env.candidate(t, direction, domain.JevEntryQualityFair))
			if err != nil {
				t.Fatalf("Evaluate fair: %v", err)
			}
			wantFair := `entry_quality_below_threshold: entry_quality="fair" < min="good"`
			if fair.Direction != domain.JevDirectionNone || reject(fair) != wantFair {
				t.Errorf("Paper + fair = %s %q, want NONE %q", fair.Direction, reject(fair), wantFair)
			}

			// The Settings screen / MCP raises the Paper value to strong: the next
			// decision uses it, good is rejected with min="strong".
			src.s = paperpolicysettings.Settings{LongMinEntryQuality: "strong", ShortMinEntryQuality: "strong"}
			raised, err := e.Evaluate(ctx, env.candidate(t, direction, domain.JevEntryQualityGood))
			if err != nil {
				t.Fatalf("Evaluate raised: %v", err)
			}
			if wantRaised := `entry_quality_below_threshold: entry_quality="good" < min="strong"`; reject(raised) != wantRaised {
				t.Errorf("after raising the Paper value: %q, want %q", reject(raised), wantRaised)
			}
		})
	}
}

// The long and short Paper values are independent.
func TestPolicy_PaperLongAndShortMinEntryQualityAreIndependent(t *testing.T) {
	env := newPaperEQEnv(t)
	src := &source{s: paperpolicysettings.Settings{LongMinEntryQuality: "fair", ShortMinEntryQuality: "strong"}}
	e := env.engine(metalabelsettings.ModePaper, src)
	ctx := context.Background()

	long, err := e.Evaluate(ctx, env.candidate(t, domain.JevDirectionLong, domain.JevEntryQualityFair))
	if err != nil || long.Direction != domain.JevDirectionLong {
		t.Errorf("LONG fair with Paper long min fair = %+v, %v; want LONG", long, err)
	}
	short, err := e.Evaluate(ctx, env.candidate(t, domain.JevDirectionShort, domain.JevEntryQualityGood))
	if err != nil {
		t.Fatal(err)
	}
	if want := `entry_quality_below_threshold: entry_quality="good" < min="strong"`; reject(short) != want {
		t.Errorf("SHORT good with Paper short min strong: %q, want %q", reject(short), want)
	}
}

// Live (any mode other than Paper): policy.long|short.min_entry_quality (here
// strong) decides, good is rejected with min="strong", and the Paper keys are
// not even read.
func TestPolicy_LiveKeepsStrong(t *testing.T) {
	env := newPaperEQEnv(t)
	src := &source{s: paperpolicysettings.Defaults()}
	e := env.engine(metalabelsettings.ModeLive, src)
	ctx := context.Background()

	good, err := e.Evaluate(ctx, env.candidate(t, domain.JevDirectionLong, domain.JevEntryQualityGood))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if want := `entry_quality_below_threshold: entry_quality="good" < min="strong"`; good.Direction != domain.JevDirectionNone || reject(good) != want {
		t.Errorf("Live + good = %s %q, want NONE %q", good.Direction, reject(good), want)
	}
	strong, err := e.Evaluate(ctx, env.candidate(t, domain.JevDirectionLong, domain.JevEntryQualityStrong))
	if err != nil || strong.Direction != domain.JevDirectionLong || strong.RejectReason != nil {
		t.Errorf("Live + strong = %+v, %v; want LONG", strong, err)
	}
	if src.reads != 0 {
		t.Errorf("the Paper keys were read %d times in Live, want 0", src.reads)
	}
}

// The applied Self-Improvement version still reaches trade_signals.policy_version
// and Live thresholds other than min_entry_quality are untouched in Paper.
func TestPolicy_PaperKeepsAppliedVersionAndOtherThresholds(t *testing.T) {
	p := paperentryquality.Policy{Inner: inner{applied: "sol-12"}, Mode: metalabelsettings.ModePaper, Source: &source{s: paperpolicysettings.Defaults()}}
	if v, err := p.AppliedPolicyVersion(context.Background()); err != nil || v != "sol-12" {
		t.Errorf("AppliedPolicyVersion = %q, %v; want sol-12", v, err)
	}
	got, err := p.CurrentThresholds(context.Background())
	want := testThresholds().Policy
	want.Long.MinEntryQuality, want.Short.MinEntryQuality = "good", "good"
	if err != nil || got != want {
		t.Errorf("CurrentThresholds = %+v, %v; want only min_entry_quality changed: %+v", got, err, want)
	}
}

// A failing read of the Paper value is an error, not a silent fallback to a
// looser or stricter threshold.
func TestPolicy_PaperEntryQualityReadFailureIsAnError(t *testing.T) {
	env := newPaperEQEnv(t)
	boom := errors.New("read failed")
	e := env.engine(metalabelsettings.ModePaper, &source{err: boom})
	if _, err := e.Evaluate(context.Background(), env.candidate(t, domain.JevDirectionLong, domain.JevEntryQualityGood)); !errors.Is(err, boom) {
		t.Fatalf("Evaluate error = %v, want %v", err, boom)
	}
}

// The SELECT of issue #861 on the test database: the Paper good candidate's
// trade_signals row has no entry_quality_below_threshold reject_reason; the
// fair one has. (jev_decisions has no entry_quality column; the answer is in
// response_json.) The rows are logged (go test -v) as evidence.
func TestPolicy_PaperGoodCandidateAdvancesInTheDatabase(t *testing.T) {
	env := newPaperEQEnv(t)
	e := env.engine(metalabelsettings.ModePaper, &source{s: paperpolicysettings.Defaults()})
	ctx := context.Background()
	for _, q := range []string{domain.JevEntryQualityGood, domain.JevEntryQualityFair, domain.JevEntryQualityGood} {
		if _, err := e.Evaluate(ctx, env.candidate(t, domain.JevDirectionLong, q)); err != nil {
			t.Fatalf("Evaluate %s: %v", q, err)
		}
	}
	rows, err := env.db.QueryContext(ctx, `
SELECT ts.id, ts.direction, ts.risk_passed, COALESCE(ts.reject_reason, 'NULL'), json_extract(jd.response_json, '$.entry_quality') AS entry_quality
FROM trade_signals ts JOIN jev_decisions jd ON jd.id = ts.jev_decision_id
ORDER BY ts.id DESC LIMIT 20`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	t.Log("id | direction | risk_passed | reject_reason | entry_quality")
	for rows.Next() {
		var id, riskPassed int
		var direction, reason, quality string
		if err := rows.Scan(&id, &direction, &riskPassed, &reason, &quality); err != nil {
			t.Fatal(err)
		}
		line := fmt.Sprintf("%d | %s | %d | %s | %s", id, direction, riskPassed, reason, quality)
		t.Log(line)
		out = append(out, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("selected %d rows, want 3", len(out))
	}
	for _, line := range out {
		advanced := strings.HasSuffix(line, "| good")
		below := strings.Contains(line, "entry_quality_below_threshold")
		if advanced && below {
			t.Errorf("a good candidate was rejected on entry quality: %s", line)
		}
		if !advanced && !below {
			t.Errorf("the fair candidate was not rejected on entry quality: %s", line)
		}
	}
}
