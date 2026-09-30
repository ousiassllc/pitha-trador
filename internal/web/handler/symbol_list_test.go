package handler_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

func TestSymbolHandler_APIPositions_ReturnsItems(t *testing.T) {
	provider := &fakeSymbolProvider{positions: []domain.Position{
		{ID: 1, Symbol: "7203", Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 2100, CurrentPrice: 2110, OpenedAt: time.Now().UTC()},
	}}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	_, api := humatest.New(t)
	huma.Get(api, "/positions", h.APIPositions)

	resp := api.Get("/positions")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"symbol":"7203"`) {
		t.Fatalf("body = %s, want it to contain the fake position", resp.Body.String())
	}
}

func TestSymbolHandler_APIOrders_FiltersByStatus(t *testing.T) {
	provider := &fakeSymbolProvider{orders: []domain.PaperOrder{
		{ID: 1, Symbol: "7203", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusFilled, SubmittedAt: time.Now().UTC()},
	}}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	_, api := humatest.New(t)
	huma.Get(api, "/orders", h.APIOrders)

	resp := api.Get("/orders?status=FILLED")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"status":"FILLED"`) {
		t.Fatalf("body = %s, want it to contain the fake order", resp.Body.String())
	}
}

func TestSymbolHandler_ListEndpoints_RejectInvalidQuery(t *testing.T) {
	h := handler.NewSymbolHandler(&fakeSymbolProvider{}, handler.SymbolRiskParams{})
	_, api := humatest.New(t)
	huma.Get(api, "/positions", h.APIPositions)
	huma.Get(api, "/orders", h.APIOrders)

	for _, path := range []string{
		"/positions?limit=0", "/positions?limit=-1", "/positions?limit=501", "/positions?limit=abc",
		"/orders?limit=0", "/orders?limit=-5", "/orders?limit=501", "/orders?status=BOGUS", "/orders?status=filled",
	} {
		if resp := api.Get(path); resp.Code != http.StatusUnprocessableEntity {
			t.Fatalf("GET %s status = %d, want 422", path, resp.Code)
		}
	}
	for _, path := range []string{"/positions?limit=500", "/orders?limit=1&status=REJECTED", "/orders?status=PENDING"} {
		if resp := api.Get(path); resp.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, resp.Code)
		}
	}
}
