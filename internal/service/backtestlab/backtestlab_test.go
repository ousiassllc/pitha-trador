package backtestlab_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/variant"
	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab"
	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab/labtest"
)

func req(t *testing.T, variants []backtestlab.Variant, extra []float64) backtestlab.Request {
	t.Helper()
	r, err := backtestlab.Normalize(variants, extra)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	return r
}

func TestNormalize_DefaultsAndLabels(t *testing.T) {
	r := req(t, nil, nil)
	if len(r.Variants) != 1 || r.Variants[0].Label != "baseline" || !r.Variants[0].Params.IsBaseline() {
		t.Fatalf("no variants should be one baseline set, got %+v", r.Variants)
	}
	if len(r.ExtraCostBps) != 4 || r.ExtraCostBps[3] != 10 {
		t.Fatalf("default extra costs = %v", r.ExtraCostBps)
	}
	r = req(t, []backtestlab.Variant{{}, {Params: variant.Params{TakeProfitPct: new(0.3)}}, {Label: "mine"}}, []float64{1})
	if got := []string{r.Variants[0].Label, r.Variants[1].Label, r.Variants[2].Label}; got[0] != "baseline" || got[1] != "v2" || got[2] != "mine" {
		t.Fatalf("labels = %v", got)
	}
}

func TestNormalize_Refusals(t *testing.T) {
	var many []backtestlab.Variant
	for range backtestlab.MaxVariants + 1 {
		many = append(many, backtestlab.Variant{})
	}
	if _, err := backtestlab.Normalize(many, nil); !errors.Is(err, backtestlab.ErrTooManyVariants) {
		t.Errorf("13 variants: %v, want ErrTooManyVariants", err)
	}
	if _, err := backtestlab.Normalize(many[:backtestlab.MaxVariants], nil); err != nil {
		t.Errorf("12 variants refused: %v", err)
	}
	if _, err := backtestlab.Normalize(nil, make([]float64, backtestlab.MaxExtraCostPoints+1)); !errors.Is(err, backtestlab.ErrTooManyExtraCosts) {
		t.Errorf("7 extra costs: %v, want ErrTooManyExtraCosts", err)
	}
	cases := map[string]struct {
		variants []backtestlab.Variant
		extra    []float64
		field    string
	}{
		"bad param":     {[]backtestlab.Variant{{}, {Params: variant.Params{StopLossPct: new(-1.0)}}}, nil, "variants[1].params.stop_loss_pct"},
		"bad rule":      {[]backtestlab.Variant{{Params: variant.Params{EntryRule: new("x")}}}, nil, "variants[0].params.entry_rule"},
		"long label":    {[]backtestlab.Variant{{Label: strings.Repeat("あ", 41)}}, nil, "variants[0].label"},
		"negative cost": {nil, []float64{0, -1}, "extra_cost_bps[1]"},
		"huge cost":     {nil, []float64{201}, "extra_cost_bps[0]"},
	}
	for name, c := range cases {
		_, err := backtestlab.Normalize(c.variants, c.extra)
		var ie *backtestlab.InvalidError
		if !errors.As(err, &ie) || ie.Field != c.field {
			t.Errorf("%s: %v, want an InvalidError for %s", name, err, c.field)
			continue
		}
		if strings.Contains(ie.Error(), "-1") {
			t.Errorf("%s: the error quotes the caller's value: %v", name, ie)
		}
	}
	if _, err := backtestlab.Normalize([]backtestlab.Variant{{Label: strings.Repeat("あ", 40)}}, nil); err != nil {
		t.Errorf("a 40-character label was refused: %v", err)
	}
}

func run(t *testing.T, r backtestlab.Request) (backtestlab.Result, backtestlab.Coverage) {
	t.Helper()
	res, cov, err := backtestlab.Run(context.Background(), &labtest.Source{}, labtest.Period(), r)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res, cov
}

func TestRun_ResultOfARealReplay(t *testing.T) {
	r := req(t, []backtestlab.Variant{{}}, nil)
	res, cov := run(t, r)
	v := res.Variants[0]
	if v.TradeCount == 0 || v.WinRate == nil || v.ExpectancyPct == nil {
		t.Fatalf("the fixture traded nothing: %+v", v)
	}
	if *v.WinRate <= 0 || *v.WinRate >= 1 {
		t.Errorf("win rate %v: one rising and one falling instrument should give a share strictly between 0 and 1", *v.WinRate)
	}
	if len(v.Days) != 2 || v.Days[0].Date != "2026-10-05" || v.Days[1].Date != "2026-10-06" {
		t.Errorf("days = %+v, want the two JST exit days", v.Days)
	}
	sumTrades, sumNet := 0, 0.0
	for _, d := range v.Days {
		sumTrades += d.TradeCount
		sumNet += d.NetPnLPct
	}
	if sumTrades != v.TradeCount {
		t.Errorf("per-day trades add to %d, want %d", sumTrades, v.TradeCount)
	}
	if diff := sumNet - v.NetPnLPct; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("per-day net adds to %v, want %v", sumNet, v.NetPnLPct)
	}
	if !v.ExpectancyCI.HasCI || v.ExpectancyCI.Lo == nil || v.ExpectancyCI.Hi == nil || v.ExpectancyCI.Days != 2 || v.ExpectancyCI.Level != 0.95 {
		t.Errorf("CI = %+v", v.ExpectancyCI)
	}
	if lo, hi := *v.ExpectancyCI.Lo, *v.ExpectancyCI.Hi; lo > *v.ExpectancyPct || hi < *v.ExpectancyPct {
		t.Errorf("CI [%v, %v] does not contain the mean %v", lo, hi, *v.ExpectancyPct)
	}
	if len(v.Sensitivity) != 4 || v.Sensitivity[0].ExpectancyPct != *v.ExpectancyPct || v.Sensitivity[3].ExpectancyPct >= v.Sensitivity[0].ExpectancyPct {
		t.Errorf("sensitivity = %+v: the 0 bps row is the result itself and more cost lowers it", v.Sensitivity)
	}
	if cov.FirstSnapshotAt == nil || !cov.FirstSnapshotAt.Equal(labtest.Day1) || cov.LastSnapshotAt == nil || !cov.LastSnapshotAt.Equal(labtest.Day2.Add(15*60*1e9)) {
		t.Errorf("coverage = %v .. %v", cov.FirstSnapshotAt, cov.LastSnapshotAt)
	}
}

// Same input, same bytes: the whole result (the CI included) is deterministic.
func TestRun_IsDeterministic(t *testing.T) {
	r := req(t, []backtestlab.Variant{{}, {Label: "tp", Params: variant.Params{TakeProfitPct: new(0.3)}}}, nil)
	var out [2][]byte
	for i := range out {
		res, _ := run(t, r)
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = b
	}
	if !bytes.Equal(out[0], out[1]) {
		t.Fatalf("two runs differ:\n%s\n%s", out[0], out[1])
	}
}

func TestRun_VariantsDifferWhenParamsDoAndInputIsNotMutated(t *testing.T) {
	r := req(t, []backtestlab.Variant{{}, {Label: "tp", Params: variant.Params{TakeProfitPct: new(0.3)}}, {Label: "again"}}, []float64{0, 5})
	src := &labtest.Source{}
	res, _, err := backtestlab.Run(context.Background(), src, labtest.Period(), r)
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := res.Variants[0], res.Variants[1], res.Variants[2]
	if *a.ExpectancyPct == *b.ExpectancyPct && a.TradeCount == b.TradeCount {
		t.Errorf("a different take profit gave the same result: %+v vs %+v", a, b)
	}
	if *a.ExpectancyPct != *c.ExpectancyPct || a.TradeCount != c.TradeCount || a.MaxDrawdownPct != c.MaxDrawdownPct {
		t.Errorf("the baseline replayed after another variant changed: %+v vs %+v (a variant leaked into the shared configs)", a, c)
	}
	if src.Loads() != 1 {
		t.Errorf("the source was loaded %d times, want once per run", src.Loads())
	}
	if len(a.Sensitivity) != 2 {
		t.Errorf("sensitivity rows = %d, want the 2 asked for", len(a.Sensitivity))
	}
}

func TestRun_ReferenceBestIsOneVariantOnly(t *testing.T) {
	r := req(t, []backtestlab.Variant{{}, {Params: variant.Params{TakeProfitPct: new(0.3)}}, {Params: variant.Params{TakeProfitPct: new(0.6)}}}, nil)
	res, _ := run(t, r)
	flagged, best := 0, -1
	for i, v := range res.Variants {
		if v.IsReferenceBest {
			flagged++
			best = i
		}
	}
	if flagged != 1 {
		t.Fatalf("%d variants flagged as the reference best, want exactly 1", flagged)
	}
	for i, v := range res.Variants {
		if *v.ExpectancyPct > *res.Variants[best].ExpectancyPct {
			t.Errorf("variant %d (%v) beats the flagged variant %d (%v)", i, *v.ExpectancyPct, best, *res.Variants[best].ExpectancyPct)
		}
	}
}

type emptySource struct{}

func (emptySource) RunConfigs(context.Context, backtest.Period) ([]backtest.RunConfig, error) {
	return nil, nil
}

// cancelOnDecision cancels the run's context at the first decision lookup of
// the replay, so the engine is in the middle of a variant when it is done.
type cancelOnDecision struct {
	backtest.DecisionSource
	cancel context.CancelFunc
}

func (c cancelOnDecision) Decision(ctx context.Context, id int64, ts time.Time) (domain.JevDecision, bool, error) {
	c.cancel()
	return c.DecisionSource.Decision(ctx, id, ts)
}

type cancelMidReplay struct{ cancel context.CancelFunc }

func (c cancelMidReplay) RunConfigs(context.Context, backtest.Period) ([]backtest.RunConfig, error) {
	cfgs := labtest.Configs()
	for i := range cfgs {
		cfgs[i].Decisions = cancelOnDecision{cfgs[i].Decisions, c.cancel}
	}
	return cfgs, nil
}

func TestRun_NoDataIsAnEmptyResultNotAnError(t *testing.T) {
	r := req(t, nil, nil)
	res, cov, err := backtestlab.Run(context.Background(), emptySource{}, labtest.Period(), r)
	if err != nil {
		t.Fatal(err)
	}
	v := res.Variants[0]
	if v.TradeCount != 0 || v.WinRate != nil || v.ExpectancyPct != nil || v.ExpectancyCI.HasCI || v.IsReferenceBest || len(v.Days) != 0 {
		t.Errorf("no data: %+v", v)
	}
	if cov.FirstSnapshotAt != nil || cov.LastSnapshotAt != nil {
		t.Errorf("coverage of no data = %+v", cov)
	}
	if b, err := json.Marshal(res); err != nil || !strings.Contains(string(b), `"days":[]`) {
		t.Errorf("empty slices must encode as [] not null: %s %v", b, err)
	}
}

func TestRun_StopsWhenContextIsDone(t *testing.T) {
	r := req(t, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := backtestlab.Run(ctx, &labtest.Source{}, labtest.Period(), r); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run with a done context = %v, want context.Canceled", err)
	}
}

// The replay engine asks the decision source at every bar; the lab's wrapper
// turns a done context into an error there, so a run stops in the middle of a
// variant, not only between variants.
func TestRun_StopsInTheMiddleOfAReplay(t *testing.T) {
	r := req(t, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	src := cancelMidReplay{cancel: cancel}
	_, _, err := backtestlab.Run(ctx, src, labtest.Period(), r)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "replay variant 0") {
		t.Fatalf("Run = %v, want context.Canceled from inside the replay of variant 0", err)
	}
}
