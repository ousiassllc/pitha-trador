package evalprovider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/evalprovider"
	"github.com/ousiassllc/pitha-trador/internal/repository/actuals/actualstest"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/insightapi"
)

// The real wiring end to end: the provider reads the synthetic database, and
// both the JSON route and the Performance page show its hand-computed
// figures (6 trades, -1,000 JPY, max drawdown 1,760 JPY of a 100,000 JPY
// capital) as 判定不能 for lack of samples.
func TestRouter_ServesEvaluationOfDatabase(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	actualstest.Seed(t, db)

	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithInsightProvider(evalprovider.New(insightapi.StaticProvider{}, db, func(context.Context) float64 { return actualstest.Capital })))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/performance/evaluation?from=2026-10-12&to=2026-10-14", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		InitialCapital float64 `json:"initial_capital"`
		Overall        struct {
			N              int     `json:"n"`
			NetPnL         float64 `json:"net_pnl_jpy"`
			MaxDrawdownJPY struct {
				Value    float64 `json:"value"`
				Reliable bool    `json:"reliable"`
			} `json:"max_drawdown_jpy"`
		} `json:"overall"`
		Verdict struct {
			Status string `json:"status"`
		} `json:"verdict"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Overall.N != 6 || body.Overall.NetPnL != -1000 || body.Overall.MaxDrawdownJPY.Value != 1760 || body.Overall.MaxDrawdownJPY.Reliable || body.InitialCapital != actualstest.Capital {
		t.Errorf("body = %+v", body)
	}
	if body.Verdict.Status != "undetermined" {
		t.Errorf("verdict = %q, want undetermined", body.Verdict.Status)
	}

	page := httptest.NewRecorder()
	engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/performance", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `data-key="2026-10-13"`) || !strings.Contains(page.Body.String(), "初期資金 ¥100,000") {
		t.Errorf("page status %d, want the evaluation section with the by-day rows and the capital", page.Code)
	}
}
