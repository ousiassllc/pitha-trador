package symbol_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/symbol"
)

func TestSymbolHandler_APICandles_RejectsInvalidRangeWithoutReadingRepository(t *testing.T) {
	for name, query := range map[string]string{
		"span over 7 days":            "from=2026-09-01T00:00:00Z&to=2026-09-27T00:00:00Z",
		"span just over 7 days":       "from=2026-09-20T00:00:00Z&to=2026-09-27T00:00:01Z",
		"epoch from, to defaults":     "from=1970-01-01T00:00:00Z",
		"from after to":               "from=2026-09-27T01:00:00Z&to=2026-09-27T00:00:00Z",
		"from after defaulted to now": "from=2999-01-01T00:00:00Z",
	} {
		t.Run(name, func(t *testing.T) {
			provider := &fakeSymbolProvider{}
			h := symbol.NewSymbolHandler(provider, symbol.SymbolRiskParams{})
			_, api := humatest.New(t)
			huma.Get(api, "/symbols/{symbol}/candles", h.APICandles)

			resp := api.Get("/symbols/7203/candles?" + query)

			if resp.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", resp.Code, resp.Body.String())
			}
			if !provider.lastCandlesFrom.IsZero() || !provider.lastCandlesTo.IsZero() {
				t.Fatalf("provider.Candles was called for a rejected range: %v..%v", provider.lastCandlesFrom, provider.lastCandlesTo)
			}
		})
	}
}

func TestSymbolHandler_APICandles_AcceptsRangeUpToSevenDays(t *testing.T) {
	provider := &fakeSymbolProvider{}
	h := symbol.NewSymbolHandler(provider, symbol.SymbolRiskParams{})
	_, api := humatest.New(t)
	huma.Get(api, "/symbols/{symbol}/candles", h.APICandles)

	resp := api.Get("/symbols/7203/candles?from=2026-09-20T00:00:00Z&to=2026-09-27T00:00:00Z")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", resp.Code, resp.Body.String())
	}
	if got := provider.lastCandlesTo.Sub(provider.lastCandlesFrom); got != 7*24*time.Hour {
		t.Fatalf("resolved window = %v, want 7d", got)
	}
}
