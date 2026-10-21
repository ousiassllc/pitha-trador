package proposaltests

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

// Every argument rule of the issue #827 table is refused with invalid_argument
// (with the rule's reason as detail, never the value) and nothing is stored.
func TestSubmitRefusesBadArguments(t *testing.T) {
	long := func(n int) string { return strings.Repeat("あ", n) }
	ok := validProposal("proposal-0001")
	cases := []struct {
		name string
		args map[string]any
	}{
		{"missing idempotency_key", without(ok, "idempotency_key")},
		{"idempotency_key too short", with(ok, "idempotency_key", "short")},
		{"idempotency_key too long", with(ok, "idempotency_key", strings.Repeat("a", 65))},
		{"idempotency_key with a space", with(ok, "idempotency_key", "has space 1")},
		{"idempotency_key with a symbol", with(ok, "idempotency_key", "key;DROP-0001")},
		{"missing symbol", without(ok, "symbol")},
		{"symbol not a code", with(ok, "symbol", "toyota")},
		{"missing bucket", without(ok, "bucket")},
		{"bucket out of range", with(ok, "bucket", "crypto")},
		{"missing action", without(ok, "action")},
		{"action out of range: open_short", with(ok, "action", "open_short")},
		{"action out of range: sell", with(ok, "action", "sell")},
		{"missing order_type", without(ok, "order_type")},
		{"order_type out of range", with(ok, "order_type", "STOP")},
		{"neither quantity nor budget_jpy", without(ok, "quantity")},
		{"both quantity and budget_jpy", with(ok, "budget_jpy", 100000)},
		{"quantity zero", with(ok, "quantity", 0)},
		{"quantity fractional", with(ok, "quantity", 1.5)},
		{"quantity negative", with(ok, "quantity", -100)},
		{"budget_jpy zero", with(without(ok, "quantity"), "budget_jpy", 0)},
		{"LIMIT without limit_price", without(ok, "limit_price")},
		{"MARKET with limit_price", with(ok, "order_type", "MARKET")},
		{"limit_price zero", with(ok, "limit_price", 0)},
		{"limit_price negative", with(ok, "limit_price", -1)},
		{"missing stop_loss_price", without(ok, "stop_loss_price")},
		{"stop_loss_price equal to limit_price", with(ok, "stop_loss_price", 2500)},
		{"stop_loss_price above limit_price", with(ok, "stop_loss_price", 2600)},
		{"take_profit_price negative", with(ok, "take_profit_price", -5)},
		{"missing exit_plan", without(ok, "exit_plan")},
		{"empty exit_plan", with(ok, "exit_plan", "  ")},
		{"exit_plan too long", with(ok, "exit_plan", long(501))},
		{"missing max_loss_jpy", without(ok, "max_loss_jpy")},
		{"max_loss_jpy zero", with(ok, "max_loss_jpy", 0)},
		{"max_loss_jpy negative", with(ok, "max_loss_jpy", -1)},
		{"missing rationale", without(ok, "rationale")},
		{"empty rationale", with(ok, "rationale", " ")},
		{"rationale too long", with(ok, "rationale", long(2001))},
		{"rationale with a newline", with(ok, "rationale", "一行目\n二行目")},
		{"rationale with a control character", with(ok, "rationale", "理由\x07")},
		{"exit_plan with a tab", with(ok, "exit_plan", "計画\t1")},
		{"missing sources", without(ok, "sources")},
		{"empty sources", with(ok, "sources", []map[string]any{})},
		{"too many sources", with(ok, "sources", manySources(11))},
		{"source without title", with(ok, "sources", []map[string]any{{"url": "https://example.com", "retrieved_at": "2026-10-18T09:30:00+09:00"}})},
		{"source url not http", with(ok, "sources", []map[string]any{{"title": "t", "url": "javascript:alert(1)", "retrieved_at": "2026-10-18T09:30:00+09:00"}})},
		{"source retrieved_at not RFC3339", with(ok, "sources", []map[string]any{{"title": "t", "url": "https://example.com", "retrieved_at": "yesterday"}})},
		{"source title with a control character", with(ok, "sources", []map[string]any{{"title": "t\n", "url": "https://example.com", "retrieved_at": "2026-10-18T09:30:00+09:00"}})},
		{"missing expires_at", without(ok, "expires_at")},
		{"expires_at not RFC3339", with(ok, "expires_at", "tomorrow")},
		{"expires_at in the past", with(ok, "expires_at", "2026-10-18T02:59:59Z")},
		{"expires_at now", with(ok, "expires_at", "2026-10-18T03:00:00Z")},
		{"expires_at beyond 24 hours", with(ok, "expires_at", "2026-10-19T03:00:01Z")},
		{"unknown argument", with(ok, "extra", 1)},
		{"mode argument (there is none)", with(ok, "mode", "paper")},
		{"live mode argument", with(ok, "mode", "live")},
		{"unknown source member", with(ok, "sources", []map[string]any{{"title": "t", "url": "https://example.com", "retrieved_at": "2026-10-18T09:30:00+09:00", "x": 1}})},
		{"string quantity", with(ok, "quantity", "100")},
		{"close with quantity", with(ok, "action", "close", "order_type", "MARKET", "limit_price", nil)},
		{"close with budget_jpy", with(without(ok, "quantity", "limit_price", "stop_loss_price", "exit_plan", "max_loss_jpy"), "action", "close", "order_type", "MARKET", "budget_jpy", 1000)},
	}
	srv, w := proposalServer(t)
	switchOn(t, w)
	before := rowCounts(t, w)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := srv.call(t, "order_proposal_submit", tc.args)
			detail := r.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
			if strings.Contains(detail, "javascript") || strings.Contains(detail, "toyota") {
				t.Errorf("the detail quotes the value: %q", detail)
			}
			wantSameCounts(t, w, before)
		})
	}
	if t.Failed() {
		return
	}
	// Positive control: the same server accepts the unchanged proposal.
	if got := submit(t, srv, ok); got.Status != "received" {
		t.Fatalf("control: %+v", got)
	}
}

func manySources(n int) []map[string]any {
	out := make([]map[string]any, n)
	for i := range out {
		out[i] = map[string]any{"title": "資料", "url": "https://example.com/a", "retrieved_at": "2026-10-18T09:30:00+09:00"}
	}
	return out
}
