package analysisflow_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/actuals"
	"github.com/ousiassllc/pitha-trador/internal/web/insightapi"
)

type verdictBody struct {
	InitialCapital float64 `json:"initial_capital"`
	Overall        struct {
		N int `json:"n"`
	} `json:"overall"`
	ByDay   []any `json:"by_day"`
	Jev     []any `json:"jev"`
	Meta    []any `json:"meta"`
	Verdict struct {
		Status string `json:"status"`
		Checks []struct {
			Key string `json:"key"`
		} `json:"checks"`
	} `json:"verdict"`
	Criteria struct {
		MinTrades int `json:"min_trades"`
	} `json:"criteria"`
}

func getEvaluation(t *testing.T, query string) (*httptest.ResponseRecorder, verdictBody) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithInsightProvider(insightapi.StaticProvider{}))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/performance/evaluation"+query, nil))
	var body verdictBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec, body
}

// The JSON of an empty database is a 判定不能 verdict with the documented
// criteria and [] (not null) for every list.
func TestAPIEvaluation_EmptyDatabase(t *testing.T) {
	rec, body := getEvaluation(t, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if body.Verdict.Status != string(actuals.StatusUndetermined) || len(body.Verdict.Checks) != 4 || body.Criteria.MinTrades != actuals.DefaultCriteria().MinTrades {
		t.Errorf("body = %+v", body)
	}
	if body.ByDay == nil || body.Jev == nil || body.Meta == nil {
		t.Errorf("lists must be [] not null: %s", rec.Body)
	}
}

func TestAPIEvaluation_RejectsBadRange(t *testing.T) {
	for _, q := range []string{"?from=2026-13-01", "?to=oops", "?from=2026-10-10&to=2026-10-01"} {
		if rec, _ := getEvaluation(t, q); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422", q, rec.Code)
		}
	}
	if rec, _ := getEvaluation(t, "?from=2026-10-01&to=2026-10-01"); rec.Code != http.StatusOK {
		t.Errorf("a one-day range: status = %d, want 200", rec.Code)
	}
}
