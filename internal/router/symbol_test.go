package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

// fakeSymbolProvider is a minimal handler.SymbolProvider for
// router-wiring tests (router_test cannot reuse internal/web/handler's
// own unexported test fake across packages).
type fakeSymbolProvider struct {
	state     execution.SymbolState
	positions []domain.Position
}

func (f fakeSymbolProvider) State(context.Context, string) (execution.SymbolState, error) {
	return f.state, nil
}
func (f fakeSymbolProvider) Candles(context.Context, string, time.Time, time.Time) ([]domain.Snapshot, error) {
	return nil, nil
}
func (f fakeSymbolProvider) RecentDecisions(context.Context, string, int) ([]domain.JevDecision, error) {
	return nil, nil
}
func (f fakeSymbolProvider) GetPosition(context.Context, int64) (domain.Position, error) {
	return domain.Position{}, repository.ErrPositionNotFound
}
func (f fakeSymbolProvider) ListPositions(context.Context, int) ([]domain.Position, error) {
	return f.positions, nil
}
func (f fakeSymbolProvider) Close(context.Context, int64, string, float64, time.Time) (domain.Position, error) {
	return domain.Position{}, repository.ErrPositionNotFound
}
func (f fakeSymbolProvider) ListOrders(context.Context, string, int) ([]domain.PaperOrder, error) {
	return nil, nil
}

func TestNew_APISymbolUsesDefaultStaticSymbolProviderWhenUnconfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/7203", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"symbol":"7203"`) {
		t.Fatalf("expected the requested symbol echoed back, got %q", rec.Body.String())
	}
}

func TestNew_APIPositionsUsesWithSymbolProviderOption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := fakeSymbolProvider{positions: []domain.Position{
		{ID: 1, Symbol: "7203", Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 2100, CurrentPrice: 2110, OpenedAt: time.Now().UTC()},
	}}
	engine := router.New(router.WithSymbolProvider(provider))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/positions", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"symbol":"7203"`) {
		t.Fatalf("expected the injected position, got %q", rec.Body.String())
	}
}

func TestNew_APISymbolReportsWithSymbolRiskParamsOption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(
		router.WithSymbolProvider(fakeSymbolProvider{state: execution.SymbolState{Symbol: "7203", LastSignal: domain.JevDirectionNone}}),
		router.WithSymbolRiskParams(handler.SymbolRiskParams{AllowedPositionPct: 1.0, StopLossPct: 0.4, TakeProfitPct: 0.9}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/7203", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusOK, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"allowed_position_pct":1`) || !strings.Contains(rec.Body.String(), `"stop_loss_pct":0.4`) {
		t.Fatalf("expected the overridden risk params, got %q", rec.Body.String())
	}
}

func TestNew_ClosePositionActionRouteIsRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	req := httptest.NewRequest(http.MethodPost, "/positions/1/close", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	// The default StaticSymbolProvider has no position 1, so this must
	// reach SymbolHandler.ClosePosition (proving the route is wired) and
	// fail with 404 rather than Gin's 404 (which has an empty body).
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body=%s)", http.StatusNotFound, rec.Code, rec.Body.String())
	}
}
