package proposaltests

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// The write path stays narrow across a whole proposal session: the only
// statements that change anything are the INSERT of one order_proposals row and
// one order_proposal_events row per accepted proposal, the cancellation request
// (one UPDATE of order_proposals.cancel_requested_at and one event row), and the
// switch-off upsert with its audit row. No statement of any kind touches
// paper_orders, positions, trade_signals or kill_switch_events, and the read
// connection still refuses writes.
func TestOrderProposalWritePathIsNarrow(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedPositions(t, w)
	switchOn(t, w)
	driverName, counter := testkit.NewCountingDriver(t)
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path, DriverName: driverName})
	before := rowCounts(t, w)

	first := submit(t, srv, validProposal("proposal-0001"))
	submit(t, srv, validProposal("proposal-0001")) // replay
	srv.call(t, "order_proposal_submit", with(validProposal("proposal-0001"), "quantity", 200)).wantError(t, http.StatusConflict, paperresultsmcp.CodeIdempotencyConflict)
	srv.call(t, "order_proposal_submit", without(validProposal("proposal-0002"), "rationale")).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	third := submit(t, srv, validProposal("proposal-0003"))
	srv.call(t, "order_proposal_cancel", map[string]any{"proposal_id": third.ProposalID}).data(t, "order_proposal_cancel", &struct{}{})
	srv.call(t, "order_proposal_cancel", map[string]any{"proposal_id": third.ProposalID}).data(t, "order_proposal_cancel", &struct{}{}) // replay
	srv.call(t, "order_proposal_cancel", map[string]any{"proposal_id": 999}).wantError(t, http.StatusNotFound, paperresultsmcp.CodeProposalNotFound)
	srv.call(t, "order_proposal_get", map[string]any{"proposal_id": first.ProposalID}).data(t, "order_proposal_get", &struct{}{})
	srv.call(t, "order_proposals_disable", map[string]any{}).data(t, "order_proposals_disable", &struct{}{})
	srv.call(t, "order_proposal_submit", validProposal("proposal-0004")).wantError(t, http.StatusForbidden, paperresultsmcp.CodeProposalsDisabled)

	forbidden := regexp.MustCompile(`(?i)\b(paper_orders|positions|trade_signals|kill_switch_events|secrets)\b`)
	changing := regexp.MustCompile(`(?i)^\s*(INSERT|UPDATE|DELETE|REPLACE|CREATE|DROP|ALTER)\b`)
	allowed := map[string]*regexp.Regexp{
		"order_proposals":       regexp.MustCompile(`(?is)^\s*(INSERT INTO order_proposals\b|UPDATE order_proposals SET cancel_requested_at = \? WHERE id = \?)`),
		"order_proposal_events": regexp.MustCompile(`(?is)^\s*INSERT INTO order_proposal_events\b`),
		"runtime_settings":      regexp.MustCompile(`(?is)^\s*INSERT INTO runtime_settings\b`),
		"mcp_settings_audit":    regexp.MustCompile(`(?is)^\s*INSERT INTO mcp_settings_audit\b`),
	}
	written := map[string]int{}
	for _, stmt := range counter.StatementLog() {
		if forbidden.MatchString(stmt) {
			t.Errorf("statement touches a table the MCP must not: %s", testkit.FirstLine(stmt))
		}
		if strings.Contains(strings.ToUpper(stmt), "SELECT *") {
			t.Errorf("SELECT *: %s", testkit.FirstLine(stmt))
		}
		if !changing.MatchString(stmt) {
			continue
		}
		matched := false
		for table, re := range allowed {
			if re.MatchString(stmt) {
				written[table]++
				matched = true
			}
		}
		if !matched {
			t.Errorf("a write statement outside the allowed set: %s", testkit.FirstLine(stmt))
		}
	}
	// Two accepted proposals (two INSERTs), one of them with a cancellation
	// request (one UPDATE; the replayed request writes nothing), one switch-off:
	// nothing else was written.
	want := map[string]int{"order_proposals": 3, "order_proposal_events": 3, "runtime_settings": 1, "mcp_settings_audit": 1}
	for table, n := range want {
		if written[table] != n {
			t.Errorf("INSERT INTO %s ran %d times, want %d", table, written[table], n)
		}
	}
	t.Logf("write statements by table: %v", written)

	after := rowCounts(t, w)
	for _, table := range []string{"paper_orders", "positions", "trade_signals", "kill_switch_events"} {
		if after[table] != before[table] {
			t.Errorf("%s changed: %d -> %d", table, before[table], after[table])
		}
	}
	// The read connection is still read-only.
	db, err := srv.DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO order_proposal_events (proposal_id, at, to_status, actor) VALUES (1, 't', 'received', 'mcp')`); err == nil {
		t.Error("the read connection accepted a write")
	}
}
