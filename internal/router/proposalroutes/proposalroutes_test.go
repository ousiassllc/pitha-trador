package proposalroutes_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proposal"
)

type oneProposal struct{}

func (oneProposal) List(context.Context, proposal.ListFilter) ([]proposal.Item, string, error) {
	return []proposal.Item{{ProposalID: 7, Status: "rejected", StatusReason: "stale_quote", ReceivedAt: "2026-10-17T00:00:00Z",
		Symbol: "7203", Bucket: "stock", Action: "open_long", OrderType: "MARKET"}}, "", nil
}

func (oneProposal) Get(_ context.Context, id int64) (proposal.State, error) {
	if id != 7 {
		return proposal.State{}, proposal.ErrNotFound
	}
	return proposal.State{ProposalID: 7, Status: "rejected", StatusReason: "stale_quote", Symbol: "7203", Bucket: "stock",
		Action: "open_long", OrderType: "MARKET", Rationale: "理由", ReceivedAt: "2026-10-17T00:00:00Z"}, nil
}

func serve(t *testing.T, engine *gin.Engine, target string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec.Code, rec.Body.String()
}

// Without a wired source the AIの注文提案 screens are empty but there: the list
// says so and marks the nav entry, and an id is a 404.
func TestNew_OrderProposalRoutesDefaultToAnEmptyList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()
	code, body := serve(t, engine, "/order-proposals")
	if code != http.StatusOK || !strings.Contains(body, `data-testid="order-proposals-empty"`) ||
		!strings.Contains(body, `href="/order-proposals" aria-current="page"`) {
		t.Fatalf("GET /order-proposals = %d: %s", code, body)
	}
	if code, _ := serve(t, engine, "/order-proposals/1"); code != http.StatusNotFound {
		t.Errorf("GET /order-proposals/1 = %d, want 404", code)
	}
}

func TestNew_OrderProposalRoutesServeTheWiredSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithOrderProposalSource(oneProposal{}))
	code, body := serve(t, engine, "/order-proposals")
	if code != http.StatusOK || !strings.Contains(body, `data-proposal-id="7"`) || !strings.Contains(body, "stale_quote") {
		t.Fatalf("GET /order-proposals = %d: %s", code, body)
	}
	code, body = serve(t, engine, "/order-proposals/7")
	if code != http.StatusOK || !strings.Contains(body, "注文提案 #7") {
		t.Fatalf("GET /order-proposals/7 = %d: %s", code, body)
	}
	// Read-only: nothing but GET is routed.
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(method, "/order-proposals/7", nil))
		if rec.Code == http.StatusOK {
			t.Errorf("%s /order-proposals/7 = 200, want it unrouted", method)
		}
	}
}
