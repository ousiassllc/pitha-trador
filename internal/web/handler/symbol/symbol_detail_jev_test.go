package symbol_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/symbol"
)

// traderDecision builds a Jev Trader jev_decisions row as enrich.Decision
// leaves it (all six Jev fields populated).
func traderDecision(direction string, confidence float64) domain.JevDecision {
	regime, quality := domain.JevRegimeBreakout, domain.JevEntryQualityStrong
	toxic, stressed := 0.18, 0.09
	return domain.JevDecision{
		DecisionType: domain.JevDecisionTypeTrader, Direction: &direction, Confidence: &confidence,
		Regime: &regime, EntryQuality: &quality, ToxicFlow: &toxic, LiquidityStressed: &stressed,
	}
}

// stateWithTrader is a SymbolState whose latest Trader decision is d.
func stateWithTrader(state execution.SymbolState, d domain.JevDecision) execution.SymbolState {
	state.LatestTraderDecision = &d
	return state
}

type symbolJevBody struct {
	VWAP *float64 `json:"vwap"`
	Jev  struct {
		Direction         *string  `json:"direction"`
		Confidence        *float64 `json:"confidence"`
		Regime            *string  `json:"regime"`
		EntryQuality      *string  `json:"entry_quality"`
		ToxicFlow         *float64 `json:"toxic_flow"`
		LiquidityStressed *float64 `json:"liquidity_stressed"`
	} `json:"jev"`
}

func getSymbolJevBody(t *testing.T, provider *fakeSymbolProvider) symbolJevBody {
	t.Helper()
	h := symbol.NewSymbolHandler(provider, symbol.SymbolRiskParams{})
	_, api := humatest.New(t)
	huma.Get(api, "/symbols/{symbol}", h.APISymbol)

	resp := api.Get("/symbols/7203")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	var body symbolJevBody
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v (body=%s)", err, resp.Body.String())
	}
	return body
}

// The jev section comes from the latest Jev Trader decision, and the latest
// Policy signal (direction/score) must not leak into it - even when it
// disagrees or is NONE. vwap comes from the latest snapshot.
func TestSymbolHandler_APISymbol_ReportsLatestTraderDecisionAndVWAP(t *testing.T) {
	vwap := 2823.0
	scout := domain.JevDecision{DecisionType: domain.JevDecisionTypeScout}
	// The decision history window is all Scout rows (60 > the 50-row
	// history limit, issues #496/#497/#499): jev must not depend on it.
	history := make([]domain.JevDecision, 60)
	for i := range history {
		history[i] = scout
	}
	provider := &fakeSymbolProvider{
		state: stateWithTrader(execution.SymbolState{
			Symbol: "7203", LastPrice: 2831.5, LastVWAP: &vwap,
			LastSignal: domain.JevDirectionNone, LastSignalConfidence: 0.99,
		}, traderDecision(domain.JevDirectionLong, 0.74)),
		decisions: history,
	}

	body := getSymbolJevBody(t, provider)

	if body.VWAP == nil || *body.VWAP != vwap {
		t.Fatalf("vwap = %v, want %v", body.VWAP, vwap)
	}
	j := body.Jev
	if j.Direction == nil || *j.Direction != domain.JevDirectionLong || j.Confidence == nil || *j.Confidence != 0.74 {
		t.Fatalf("jev direction/confidence = %v/%v, want LONG/0.74 (latest Trader decision, not the NONE/0.99 signal)", j.Direction, j.Confidence)
	}
	if j.Regime == nil || *j.Regime != domain.JevRegimeBreakout || j.EntryQuality == nil || *j.EntryQuality != domain.JevEntryQualityStrong {
		t.Fatalf("jev regime/entry_quality = %v/%v, want BREAKOUT/strong", j.Regime, j.EntryQuality)
	}
	if j.ToxicFlow == nil || *j.ToxicFlow != 0.18 || j.LiquidityStressed == nil || *j.LiquidityStressed != 0.09 {
		t.Fatalf("jev toxic_flow/liquidity_stressed = %v/%v, want 0.18/0.09", j.ToxicFlow, j.LiquidityStressed)
	}
}

// Without any Trader decision (only Scout rows) and without a snapshot, vwap
// and every jev field are null - a Policy signal alone does not fill jev.
func TestSymbolHandler_APISymbol_NullJevAndVWAPWithoutTraderDecisionOrSnapshot(t *testing.T) {
	provider := &fakeSymbolProvider{
		state:     execution.SymbolState{Symbol: "7203", LastSignal: domain.JevDirectionLong, LastSignalConfidence: 0.8},
		decisions: []domain.JevDecision{{DecisionType: domain.JevDecisionTypeScout}},
	}

	body := getSymbolJevBody(t, provider)

	if body.VWAP != nil {
		t.Fatalf("vwap = %v, want null (no snapshot)", *body.VWAP)
	}
	j := body.Jev
	if j.Direction != nil || j.Confidence != nil || j.Regime != nil || j.EntryQuality != nil || j.ToxicFlow != nil || j.LiquidityStressed != nil {
		t.Fatalf("jev = %+v, want every field null (no Trader decision)", j)
	}
}
