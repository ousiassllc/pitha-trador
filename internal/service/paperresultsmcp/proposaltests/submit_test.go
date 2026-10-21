package proposaltests

import (
	"database/sql"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// While the intake switch is off - nothing stored (the initial state), 0, or a
// damaged value - order_proposal_submit answers proposals_disabled and no table
// changes.
func TestSubmitIsRefusedWhileTheSwitchIsOff(t *testing.T) {
	for name, stored := range map[string]*string{
		"no row (initial state)": nil,
		"stored 0":               ptr("0"),
		"damaged: text":          ptr(`"yes"`),
		"damaged: 2":             ptr("2"),
		"damaged: null":          ptr("null"),
		"damaged: not JSON":      ptr("on"),
	} {
		t.Run(name, func(t *testing.T) {
			srv, w := proposalServer(t)
			if stored != nil {
				testkit.MustExec(t, w, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('mcp.order_proposals_enabled', ?, 't')`, *stored)
			}
			before := rowCounts(t, w)
			srv.call(t, "order_proposal_submit", validProposal("proposal-0001")).wantError(t, http.StatusForbidden, paperresultsmcp.CodeProposalsDisabled)
			wantSameCounts(t, w, before)
		})
	}
}

// A correct proposal puts one order_proposals row and one order_proposal_events
// row (received) into the database, with the application's link columns still
// empty, and order_proposal_get reads it back.
func TestSubmitRecordsTheProposalAndItsFirstEvent(t *testing.T) {
	srv, w := proposalServer(t)
	switchOn(t, w)
	before := rowCounts(t, w)

	got := submit(t, srv, validProposal("proposal-0001"))
	if got.ProposalID != 1 || got.Status != "received" || got.Replayed || got.ReceivedAt != "2026-10-18T03:00:00Z" || got.ExpiresAt != "2026-10-19T02:00:00Z" {
		t.Fatalf("submit = %+v", got)
	}
	after := rowCounts(t, w)
	if after["order_proposals"] != 1 || after["order_proposal_events"] != 1 {
		t.Fatalf("rows after one submit: %v", after)
	}
	for _, table := range []string{"runtime_settings", "mcp_settings_audit", "paper_orders", "positions", "trade_signals", "kill_switch_events"} {
		if after[table] != before[table] {
			t.Errorf("%s changed: %d -> %d", table, before[table], after[table])
		}
	}

	var (
		key, hash, received, client, mode, symbol, bucket, action, orderType, status, reason, expires, updated, rationale, sources string
		qty, instrument, signal, order, position                                                                                   sql.NullInt64
		limit, stop, loss                                                                                                          sql.NullFloat64
		budget, take                                                                                                               sql.NullFloat64
		plan                                                                                                                       sql.NullString
	)
	err := w.QueryRowContext(t.Context(), `SELECT idempotency_key, request_hash, received_at, client, mode, symbol, bucket, action, quantity,
		budget_jpy, order_type, limit_price, stop_loss_price, take_profit_price, exit_plan, max_loss_jpy, rationale, sources_json,
		expires_at, status, status_reason, instrument_id, trade_signal_id, paper_order_id, position_id, updated_at FROM order_proposals`).Scan(
		&key, &hash, &received, &client, &mode, &symbol, &bucket, &action, &qty, &budget, &orderType, &limit, &stop, &take, &plan, &loss,
		&rationale, &sources, &expires, &status, &reason, &instrument, &signal, &order, &position, &updated)
	if err != nil {
		t.Fatal(err)
	}
	if key != "proposal-0001" || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(hash) || mode != "paper" || symbol != "7203" || bucket != "stock" ||
		action != "open_long" || orderType != "LIMIT" || status != "received" || reason != "" || client != "127.0.0.1" {
		t.Errorf("row: key=%q hash=%q mode=%q symbol=%q bucket=%q action=%q type=%q status=%q reason=%q client=%q", key, hash, mode, symbol, bucket, action, orderType, status, reason, client)
	}
	if !qty.Valid || qty.Int64 != 100 || budget.Valid || !limit.Valid || limit.Float64 != 2500 || !stop.Valid || stop.Float64 != 2450 || take.Valid ||
		!plan.Valid || plan.String != "決算発表前の引けで手じまう" || !loss.Valid || loss.Float64 != 5000 {
		t.Errorf("numbers: qty=%v budget=%v limit=%v stop=%v take=%v plan=%v loss=%v", qty, budget, limit, stop, take, plan, loss)
	}
	if instrument.Valid || signal.Valid || order.Valid || position.Valid {
		t.Errorf("link columns are filled by the MCP: %v %v %v %v", instrument, signal, order, position)
	}
	if received != updated || !strings.HasPrefix(received, "2026-10-18T03:00:00") || !strings.HasPrefix(expires, "2026-10-19T02:00:00") ||
		!strings.Contains(sources, `"title":"決算短信"`) || !strings.Contains(sources, `"retrieved_at":"2026-10-18T00:30:00Z"`) || rationale == "" {
		t.Errorf("row: received=%q updated=%q expires=%q sources=%q rationale=%q", received, updated, expires, sources, rationale)
	}

	var (
		proposalID                       int64
		at, toStatus, eventReason, actor string
		from                             sql.NullString
	)
	if err := w.QueryRowContext(t.Context(), `SELECT proposal_id, at, from_status, to_status, reason_code, actor FROM order_proposal_events`).
		Scan(&proposalID, &at, &from, &toStatus, &eventReason, &actor); err != nil {
		t.Fatal(err)
	}
	if proposalID != 1 || from.Valid || toStatus != "received" || eventReason != "" || actor != "mcp" || at != received {
		t.Errorf("event: proposal=%d at=%q from=%v to=%q reason=%q actor=%q", proposalID, at, from, toStatus, eventReason, actor)
	}

	var p struct {
		ProposalID    int64    `json:"proposal_id"`
		Status        string   `json:"status"`
		StatusReason  string   `json:"status_reason"`
		Symbol        string   `json:"symbol"`
		Quantity      *int64   `json:"quantity"`
		BudgetJPY     *float64 `json:"budget_jpy"`
		StopLossPrice *float64 `json:"stop_loss_price"`
		PaperOrderID  *int64   `json:"paper_order_id"`
		Sources       []struct {
			Title string `json:"title"`
		} `json:"sources"`
		Events []struct {
			FromStatus *string `json:"from_status"`
			ToStatus   string  `json:"to_status"`
			Actor      string  `json:"actor"`
		} `json:"events"`
	}
	srv.call(t, "order_proposal_get", map[string]any{"proposal_id": got.ProposalID}).data(t, "order_proposal_get", &p)
	if p.ProposalID != 1 || p.Status != "received" || p.StatusReason != "" || p.Symbol != "7203" || p.Quantity == nil || *p.Quantity != 100 ||
		p.BudgetJPY != nil || p.StopLossPrice == nil || *p.StopLossPrice != 2450 || p.PaperOrderID != nil ||
		len(p.Sources) != 1 || p.Sources[0].Title != "決算短信" ||
		len(p.Events) != 1 || p.Events[0].FromStatus != nil || p.Events[0].ToStatus != "received" || p.Events[0].Actor != "mcp" {
		t.Errorf("order_proposal_get = %+v", p)
	}
}

// A close proposal and a budget_jpy open_long proposal are accepted too.
func TestSubmitAcceptsCloseAndBudgetProposals(t *testing.T) {
	srv, w := proposalServer(t)
	switchOn(t, w)
	closeArgs := without(validProposal("close-0000001"), "quantity", "limit_price", "stop_loss_price", "exit_plan", "max_loss_jpy")
	closeArgs["action"], closeArgs["order_type"] = "close", "MARKET"
	if got := submit(t, srv, closeArgs); got.Status != "received" {
		t.Fatalf("close = %+v", got)
	}
	budget := without(validProposal("budget-0000001"), "quantity")
	budget["budget_jpy"] = 250000
	if got := submit(t, srv, budget); got.ProposalID != 2 {
		t.Fatalf("budget = %+v", got)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM order_proposals WHERE (action = 'close' AND quantity IS NULL AND budget_jpy IS NULL AND stop_loss_price IS NULL)
		OR (action = 'open_long' AND quantity IS NULL AND budget_jpy = 250000)`); n != 2 {
		t.Fatalf("stored shapes: %d of 2 matched", n)
	}
}

// If the first event cannot be stored, the proposal row is not left behind:
// the two INSERTs are one transaction.
func TestSubmitLeavesNoRowWhenTheEventInsertFails(t *testing.T) {
	srv, w := proposalServer(t)
	switchOn(t, w)
	testkit.MustExec(t, w, `CREATE TRIGGER fail_event BEFORE INSERT ON order_proposal_events BEGIN SELECT RAISE(ABORT, 'event insert failed'); END`)
	before := rowCounts(t, w)
	r := srv.call(t, "order_proposal_submit", validProposal("proposal-0001"))
	if r.status != http.StatusInternalServerError || r.errorBody(t).Code != string(paperresultsmcp.CodeInternal) {
		t.Fatalf("a failing event insert: %d %s", r.status, r.body)
	}
	wantSameCounts(t, w, before)

	// The other order: the event is fine and the proposal INSERT fails.
	testkit.MustExec(t, w, `DROP TRIGGER fail_event`)
	testkit.MustExec(t, w, `CREATE TRIGGER fail_proposal BEFORE INSERT ON order_proposals BEGIN SELECT RAISE(ABORT, 'proposal insert failed'); END`)
	srv.call(t, "order_proposal_submit", validProposal("proposal-0002"))
	wantSameCounts(t, w, before)
}

// The same idempotency_key with the same content returns the first proposal and
// stores nothing; the same key with different content is idempotency_conflict
// and stores nothing.
func TestSameIdempotencyKeyReplaysAndDifferentContentConflicts(t *testing.T) {
	srv, w := proposalServer(t)
	switchOn(t, w)
	first := submit(t, srv, validProposal("proposal-0001"))
	before := rowCounts(t, w)

	// Spelling that does not change the content (a different zone for the same
	// instant, surrounding spaces) is the same proposal.
	again := submit(t, srv, with(validProposal("proposal-0001"), "expires_at", "2026-10-19T11:00:00+09:00", "rationale", "  直近の決算が好調で、需給も良い "))
	if again.ProposalID != first.ProposalID || !again.Replayed || again.Status != "received" || again.ReceivedAt != first.ReceivedAt {
		t.Fatalf("replay = %+v, first = %+v", again, first)
	}
	wantSameCounts(t, w, before)

	for name, changed := range map[string]map[string]any{
		"symbol":     with(validProposal("proposal-0001"), "symbol", "6758"),
		"quantity":   with(validProposal("proposal-0001"), "quantity", 200),
		"rationale":  with(validProposal("proposal-0001"), "rationale", "別の理由"),
		"expires_at": with(validProposal("proposal-0001"), "expires_at", "2026-10-19T01:00:00Z"),
		"order type": with(without(validProposal("proposal-0001"), "limit_price"), "order_type", "MARKET"),
	} {
		srv.call(t, "order_proposal_submit", changed).wantError(t, http.StatusConflict, paperresultsmcp.CodeIdempotencyConflict)
		wantSameCounts(t, w, before)
		_ = name
	}
}

// Rows keep their status after the application moves them on: a replay returns
// the current state, not "received".
func TestReplayReturnsTheCurrentStatus(t *testing.T) {
	srv, w := proposalServer(t)
	switchOn(t, w)
	first := submit(t, srv, validProposal("proposal-0001"))
	testkit.MustExec(t, w, `UPDATE order_proposals SET status = 'rejected', status_reason = 'kill_switch_active' WHERE id = ?`, first.ProposalID)
	if again := submit(t, srv, validProposal("proposal-0001")); !again.Replayed || again.Status != "rejected" || again.ProposalID != first.ProposalID {
		t.Fatalf("replay of a rejected proposal = %+v", again)
	}
}

// Shape errors win over the switch: a malformed proposal is invalid_argument
// even while intake is off (the order is shape, switch, limit, idempotency).
func TestShapeIsCheckedBeforeTheSwitch(t *testing.T) {
	srv, _ := proposalServer(t)
	srv.call(t, "order_proposal_submit", without(validProposal("proposal-0001"), "rationale")).
		wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
}

// The 21st proposal of a JST day is limit_exceeded; the JST day, not the UTC
// day, is what is counted.
func TestTwentyFirstProposalOfTheDayIsLimitExceeded(t *testing.T) {
	srv, w := proposalServer(t)
	switchOn(t, w)
	for i := range 20 {
		submit(t, srv, validProposal("proposal-"+pad(i)))
	}
	before := rowCounts(t, w)
	detail := srv.call(t, "order_proposal_submit", validProposal("proposal-0021")).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
	if !strings.Contains(detail, "20") {
		t.Errorf("limit detail = %q", detail)
	}
	wantSameCounts(t, w, before)
}

// Proposals of the previous JST day do not count (2026-10-17T14:59:59Z is
// 23:59:59 JST the day before the fixed clock's day), those from 00:00 JST
// (15:00:00Z) on do.
func TestDailyLimitCountsTheJSTDay(t *testing.T) {
	for name, tc := range map[string]struct {
		receivedAt string
		wantOK     bool
	}{
		"yesterday 23:59:59 JST": {"2026-10-17T14:59:59.000000000Z", true},
		"today 00:00:00 JST":     {"2026-10-17T15:00:00.000000000Z", false},
	} {
		t.Run(name, func(t *testing.T) {
			srv, w := proposalServer(t)
			switchOn(t, w)
			for i := range 20 {
				testkit.MustExec(t, w, `INSERT INTO order_proposals (idempotency_key, request_hash, received_at, symbol, bucket, action, order_type, rationale, sources_json, expires_at, updated_at)
					VALUES (?, 'h', ?, '7203', 'stock', 'close', 'MARKET', 'r', '[]', 't', 't')`, "seeded-"+pad(i), tc.receivedAt)
			}
			r := srv.call(t, "order_proposal_submit", validProposal("proposal-0001"))
			if tc.wantOK {
				var out submitted
				r.data(t, "order_proposal_submit", &out)
				return
			}
			r.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
		})
	}
}

func pad(i int) string { return string(rune('a'+i/10)) + string(rune('a'+i%10)) + "-00000" }

// order_proposal_get: an unknown id is proposal_not_found, a bad id is
// invalid_argument, and it works while the intake switch is off.
func TestGetNotFoundAndWhileOff(t *testing.T) {
	srv, w := proposalServer(t)
	srv.call(t, "order_proposal_get", map[string]any{"proposal_id": 1}).wantError(t, http.StatusNotFound, paperresultsmcp.CodeProposalNotFound)
	for _, args := range []map[string]any{{}, {"proposal_id": 0}, {"proposal_id": -1}, {"proposal_id": "1"}, {"proposal_id": 1, "x": 1}} {
		srv.call(t, "order_proposal_get", args).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	}
	switchOn(t, w)
	id := submit(t, srv, validProposal("proposal-0001")).ProposalID
	testkit.MustExec(t, w, `UPDATE runtime_settings SET value = '0' WHERE key = 'mcp.order_proposals_enabled'`)
	var p struct {
		Status string `json:"status"`
	}
	srv.call(t, "order_proposal_get", map[string]any{"proposal_id": id}).data(t, "order_proposal_get", &p)
	if p.Status != "received" {
		t.Fatalf("get while off = %+v", p)
	}
}

func ptr(s string) *string { return &s }
