package handler_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

func TestSymbolHandler_ClosePosition_ClosesAtLatestKnownPriceAndRendersRow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	closedAt := time.Now().UTC()
	provider := &fakeSymbolProvider{
		position: domain.Position{ID: 42, Symbol: "7203", CurrentPrice: 2090.0, OpenedAt: closedAt.Add(-time.Hour)},
		state:    execution.SymbolState{Symbol: "7203", LastPrice: 2110.0, LastSignal: domain.JevDirectionNone},
		closeResult: domain.Position{
			ID: 42, Symbol: "7203", Side: domain.PositionSideLong, Quantity: 100,
			EntryPrice: 2100.0, CurrentPrice: 2110.0, ClosedAt: &closedAt, ExitReason: strPtr(domain.ExitReasonManual),
		},
	}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	router := gin.New()
	router.POST("/positions/:id/close", h.ClosePosition)

	req := httptest.NewRequest(http.MethodPost, "/positions/42/close", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if provider.closeCalls != 1 || provider.closeReason != domain.ExitReasonManual {
		t.Fatalf("close calls/reason = %d/%q, want 1/%q", provider.closeCalls, provider.closeReason, domain.ExitReasonManual)
	}
	if !strings.Contains(rec.Body.String(), `data-position-id="42"`) {
		t.Fatalf("body = %q, want the rendered PositionRow fragment for position 42", rec.Body.String())
	}
}

func TestSymbolHandler_ClosePosition_AlreadyClosedReturns409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	closedAt := time.Now().UTC()
	provider := &fakeSymbolProvider{position: domain.Position{ID: 42, Symbol: "7203", ClosedAt: &closedAt}}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	router := gin.New()
	router.POST("/positions/:id/close", h.ClosePosition)

	req := httptest.NewRequest(http.MethodPost, "/positions/42/close", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if provider.closeCalls != 0 {
		t.Fatalf("close calls = %d, want 0 (already closed)", provider.closeCalls)
	}
}

func TestSymbolHandler_ClosePosition_NotFoundReturns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &fakeSymbolProvider{positionErr: domain.ErrPositionNotFound}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	router := gin.New()
	router.POST("/positions/:id/close", h.ClosePosition)

	req := httptest.NewRequest(http.MethodPost, "/positions/999/close", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestSymbolHandler_ClosePosition_InvalidIDReturns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &fakeSymbolProvider{}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	router := gin.New()
	router.POST("/positions/:id/close", h.ClosePosition)

	req := httptest.NewRequest(http.MethodPost, "/positions/not-a-number/close", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSymbolHandler_ClosePosition_CloseErrorReturns500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &fakeSymbolProvider{
		position: domain.Position{ID: 42, Symbol: "7203"},
		closeErr: errors.New("db unavailable"),
	}
	h := handler.NewSymbolHandler(provider, handler.SymbolRiskParams{})
	router := gin.New()
	router.POST("/positions/:id/close", h.ClosePosition)

	req := httptest.NewRequest(http.MethodPost, "/positions/42/close", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
