package insightapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/insight"
	"github.com/ousiassllc/pitha-trador/internal/web/insightapi"
)

func TestSymbolHandler_APISignals_ReturnsRiskRejectedSignal(t *testing.T) {
	provider := &fakeProvider{signals: []domain.TradeSignal{{
		ID: 3, Symbol: "7203", Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
		Direction: domain.JevDirectionLong, Score: floatPtr(0.74), PolicyVersion: "v1",
		RiskPassed: false, RejectReason: strPtr("spread_too_wide"),
	}}}
	_, api := humatest.New(t)
	insightapi.New(provider).Register(api)

	resp := api.Get("/signals")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 {
		t.Fatalf("items = %v, want 1", body.Items)
	}
	item := body.Items[0]
	if item["risk_passed"] != false || item["reject_reason"] != "spread_too_wide" || item["direction"] != "LONG" || item["symbol"] != "7203" {
		t.Fatalf("item = %v, want risk_passed=false/reject_reason=spread_too_wide/LONG/7203", item)
	}
	if provider.lastSignalsLimit != 100 {
		t.Fatalf("limit = %d, want default 100", provider.lastSignalsLimit)
	}
}

func TestSymbolHandler_APISignals_EmptyReturnsEmptyArray(t *testing.T) {
	_, api := humatest.New(t)
	insightapi.New(&fakeProvider{}).Register(api)

	resp := api.Get("/signals?limit=5")

	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"items":[]`) {
		t.Fatalf("status/body = %d/%s, want 200 with empty items array", resp.Code, resp.Body.String())
	}
}

func TestSymbolHandler_APISymbolSignals_UnknownSymbolReturns404(t *testing.T) {
	provider := &fakeProvider{signalsErr: execution.ErrInstrumentUnknown}
	_, api := humatest.New(t)
	insightapi.New(provider).Register(api)

	resp := api.Get("/signals/9999")

	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusNotFound, resp.Body.String())
	}
}

func TestSymbolHandler_APISymbolSignals_ReturnsItems(t *testing.T) {
	provider := &fakeProvider{signals: []domain.TradeSignal{{
		ID: 1, Symbol: "7203", Timestamp: time.Now().UTC(), Direction: domain.JevDirectionNone, PolicyVersion: "v1",
	}}}
	_, api := humatest.New(t)
	insightapi.New(provider).Register(api)

	resp := api.Get("/signals/7203")

	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"symbol":"7203"`) {
		t.Fatalf("status/body = %d/%s, want 200 containing the signal", resp.Code, resp.Body.String())
	}
}

func TestSymbolHandler_APIDecisions_ReturnsScoutAndTraderFields(t *testing.T) {
	provider := &fakeProvider{decisions: []domain.JevDecision{
		{ID: 2, Symbol: "7203", Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC), DecisionType: domain.JevDecisionTypeTrader,
			Direction: strPtr("LONG"), Confidence: floatPtr(0.74), Regime: strPtr("BREAKOUT"), ModelID: "m1", QuestionVersion: "v1"},
		{ID: 1, Symbol: "7203", Timestamp: time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC), DecisionType: domain.JevDecisionTypeScout, ModelID: "m1", QuestionVersion: "v1"},
	}}
	_, api := humatest.New(t)
	insightapi.New(provider).Register(api)

	resp := api.Get("/symbols/7203/decisions")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	var body struct {
		Symbol string           `json:"symbol"`
		Items  []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Symbol != "7203" || len(body.Items) != 2 {
		t.Fatalf("body = %+v, want symbol 7203 and 2 items", body)
	}
	trader, scout := body.Items[0], body.Items[1]
	if trader["decision_type"] != "trader" || trader["direction"] != "LONG" || trader["regime"] != "BREAKOUT" || trader["confidence"] != 0.74 {
		t.Fatalf("trader item = %v, want trader/LONG/BREAKOUT/0.74", trader)
	}
	if scout["decision_type"] != "scout" || scout["direction"] != nil {
		t.Fatalf("scout item = %v, want scout with null direction", scout)
	}
}

func TestSymbolHandler_APIDecisions_UnknownSymbolReturns404(t *testing.T) {
	provider := &fakeProvider{decisionsErr: execution.ErrInstrumentUnknown}
	_, api := humatest.New(t)
	insightapi.New(provider).Register(api)

	resp := api.Get("/symbols/9999/decisions")

	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusNotFound, resp.Body.String())
	}
}

func TestSymbolHandler_APIPerformance_MapsFieldsAndNullsUndefinedRatios(t *testing.T) {
	pf := 1.82
	provider := &fakeProvider{performance: insight.Performance{
		TotalPnL: 128340, DailyPnL: 15200, TradeCount: 12, WinRate: 0.57, ProfitFactor: &pf,
		Expectancy: 0.34, MaxDrawdownPct: 4.1, AverageHoldTimeMinutes: 14.2, SignalCount: 342,
	}}
	_, api := humatest.New(t)
	insightapi.New(provider).Register(api)

	resp := api.Get("/performance")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]any{
		"total_pnl": 128340.0, "daily_pnl": 15200.0, "win_rate": 0.57, "profit_factor": 1.82, "expectancy": 0.34,
		"max_drawdown_pct": 4.1, "average_hold_time_minutes": 14.2, "signal_count": 342.0, "sharpe_ref": nil, "sortino_ref": nil,
	}
	for k, v := range want {
		got, ok := body[k]
		if !ok || got != v {
			t.Fatalf("body[%q] = %v (present=%v), want %v (body=%s)", k, got, ok, v, resp.Body.String())
		}
	}
}

func TestSymbolHandler_APIPerformance_ProviderErrorReturns500(t *testing.T) {
	provider := &fakeProvider{perfErr: execution.ErrInstrumentUnknown}
	_, api := humatest.New(t)
	insightapi.New(provider).Register(api)

	resp := api.Get("/performance")

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusInternalServerError)
	}
}

func TestHandler_InvalidInput_Returns422WithoutQueryingProvider(t *testing.T) {
	for _, path := range []string{
		"/signals?limit=0", "/signals?limit=-1", "/signals?limit=501", "/signals?limit=100000",
		"/signals/7203?limit=0", "/signals/7203?limit=501", "/signals/..x",
		"/symbols/7203/decisions?limit=0", "/symbols/7203/decisions?limit=501",
		"/symbols/abcdefghijklmnopq/decisions", "/symbols/72-03/decisions",
	} {
		t.Run(path, func(t *testing.T) {
			provider := &fakeProvider{}
			_, api := humatest.New(t)
			insightapi.New(provider).Register(api)

			resp := api.Get(path)

			if resp.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusUnprocessableEntity, resp.Body.String())
			}
			if provider.calls != 0 {
				t.Fatalf("provider called %d times, want 0", provider.calls)
			}
		})
	}
}

func TestHandler_LimitBoundaries_PassedThrough(t *testing.T) {
	for _, limit := range []string{"1", "500"} {
		provider := &fakeProvider{}
		_, api := humatest.New(t)
		insightapi.New(provider).Register(api)

		resp := api.Get("/signals?limit=" + limit)

		if resp.Code != http.StatusOK || strconv.Itoa(provider.lastSignalsLimit) != limit {
			t.Fatalf("limit=%s: status/limit = %d/%d, want 200 and the same limit", limit, resp.Code, provider.lastSignalsLimit)
		}
	}
}

func floatPtr(f float64) *float64 { return &f }
func strPtr(s string) *string     { return &s }

type fakeProvider struct {
	decisions    []domain.JevDecision
	decisionsErr error

	signals          []domain.TradeSignal
	signalsErr       error
	lastSignalsLimit int
	calls            int

	performance insight.Performance
	perfErr     error
}

func (f *fakeProvider) RecentDecisions(context.Context, string, int) ([]domain.JevDecision, error) {
	f.calls++
	return f.decisions, f.decisionsErr
}

func (f *fakeProvider) ListSignals(_ context.Context, limit int) ([]domain.TradeSignal, error) {
	f.calls++
	f.lastSignalsLimit = limit
	return f.signals, f.signalsErr
}

func (f *fakeProvider) RecentSignals(_ context.Context, _ string, limit int) ([]domain.TradeSignal, error) {
	f.calls++
	f.lastSignalsLimit = limit
	return f.signals, f.signalsErr
}

func (f *fakeProvider) Performance(context.Context, time.Time) (insight.Performance, error) {
	return f.performance, f.perfErr
}
