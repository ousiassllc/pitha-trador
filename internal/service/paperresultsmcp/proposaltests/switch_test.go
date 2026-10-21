package proposaltests

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// The MCP can switch the intake off (and the switch change is audited), and
// that is all it can do: the next submit is proposals_disabled, proposals
// already recorded stay readable, and the write switch is untouched.
func TestOrderProposalsDisableTurnsTheIntakeOffAndIsAudited(t *testing.T) {
	srv, w := proposalServer(t)
	switchOn(t, w)
	id := submit(t, srv, validProposal("proposal-0001")).ProposalID

	var out struct {
		Enabled bool   `json:"order_proposals_enabled"`
		AuditID int64  `json:"audit_id"`
		At      string `json:"at"`
	}
	srv.call(t, "order_proposals_disable", map[string]any{}).data(t, "order_proposals_disable", &out)
	if out.Enabled || out.AuditID != 1 || out.At != "2026-10-18T03:00:00Z" {
		t.Fatalf("order_proposals_disable = %+v", out)
	}
	if v, ok := stored(t, w, "mcp.order_proposals_enabled"); !ok || v != "0" {
		t.Fatalf("stored switch = %q, %v", v, ok)
	}
	if _, ok := stored(t, w, "mcp.settings_write_enabled"); ok {
		t.Error("turning the intake off wrote the write switch")
	}

	var actor, client, action, key string
	var before, after *string
	if err := w.QueryRowContext(t.Context(), `SELECT actor, client, action, setting_key, before_value, after_value FROM mcp_settings_audit`).
		Scan(&actor, &client, &action, &key, &before, &after); err != nil {
		t.Fatal(err)
	}
	if actor != "mcp" || client != "127.0.0.1" || action != "order_proposals_disable" || key != "mcp.order_proposals_enabled" ||
		before == nil || *before != "1" || after == nil || *after != "0" {
		t.Errorf("audit row: %s %s %s %s %v %v", actor, client, action, key, before, after)
	}

	rows := rowCounts(t, w)
	srv.call(t, "order_proposal_submit", validProposal("proposal-0002")).wantError(t, http.StatusForbidden, paperresultsmcp.CodeProposalsDisabled)
	wantSameCounts(t, w, rows)
	var p struct {
		Status string `json:"status"`
	}
	srv.call(t, "order_proposal_get", map[string]any{"proposal_id": id}).data(t, "order_proposal_get", &p)
	if p.Status != "received" {
		t.Errorf("get after disable = %+v", p)
	}

	// Switching off again is allowed (and audited once more); settings_audit lists the rows.
	srv.call(t, "order_proposals_disable", map[string]any{}).data(t, "order_proposals_disable", &out)
	if out.AuditID != 2 {
		t.Errorf("second disable audit id = %d", out.AuditID)
	}
	var audit struct {
		Entries []struct {
			Actor  string  `json:"actor"`
			Action string  `json:"action"`
			Key    string  `json:"key"`
			Before *string `json:"before"`
			After  *string `json:"after"`
		} `json:"entries"`
	}
	srv.call(t, "settings_audit", map[string]any{}).data(t, "settings_audit", &audit)
	if len(audit.Entries) != 2 || audit.Entries[0].Action != "order_proposals_disable" || audit.Entries[0].Key != "mcp.order_proposals_enabled" ||
		audit.Entries[0].Before == nil || *audit.Entries[0].Before != "0" {
		t.Errorf("settings_audit = %+v", audit.Entries)
	}
}

// order_proposals_disable takes no arguments.
func TestOrderProposalsDisableRejectsArguments(t *testing.T) {
	srv, w := proposalServer(t)
	switchOn(t, w)
	srv.call(t, "order_proposals_disable", map[string]any{"enabled": true}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	if v, _ := stored(t, w, "mcp.order_proposals_enabled"); v != "1" {
		t.Fatalf("a refused call changed the switch to %q", v)
	}
}

// Nothing the MCP can call turns the intake on: settings_set refuses the key
// (key_not_allowed, for strings and numbers alike), the write switch is
// another value, settings_get does not list the key, and the tool list has no
// enabling tool.
func TestNothingTurnsTheIntakeOnFromTheMCP(t *testing.T) {
	srv, w := proposalServer(t)
	for _, value := range []any{1, "1", "on", true} {
		srv.call(t, "settings_set", map[string]any{"key": "mcp.order_proposals_enabled", "value": value}).
			wantError(t, http.StatusForbidden, paperresultsmcp.CodeKeyNotAllowed)
	}
	srv.call(t, "settings_write_disable", map[string]any{}).data(t, "settings_write_disable", &struct{}{})
	srv.call(t, "settings_write_enable", map[string]any{}).data(t, "settings_write_enable", &struct{}{})
	srv.call(t, "order_proposals_disable", map[string]any{}).data(t, "order_proposals_disable", &struct{}{})
	if v, ok := stored(t, w, "mcp.order_proposals_enabled"); !ok || v != "0" {
		t.Fatalf("after every MCP call the switch is %q (stored %v), want 0", v, ok)
	}
	srv.call(t, "order_proposal_submit", validProposal("proposal-0001")).wantError(t, http.StatusForbidden, paperresultsmcp.CodeProposalsDisabled)

	r := srv.call(t, "settings_get", map[string]any{})
	if strings.Contains(string(r.body), "order_proposals_enabled") {
		t.Error("settings_get lists the intake switch as a setting")
	}
	list := srv.post(t, "Bearer "+testAPIKey, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	for _, name := range regexp.MustCompile(`"name":"([a-z_]+)"`).FindAllStringSubmatch(string(list.body), -1) {
		if strings.Contains(name[1], "proposal") && !strings.HasPrefix(name[1], "order_proposal") {
			t.Errorf("unexpected tool %q", name[1])
		}
		if strings.Contains(name[1], "enable") && !strings.HasPrefix(name[1], "settings_write_") {
			t.Errorf("tool %q looks like an enabling tool", name[1])
		}
	}
	if n := count(t, w, `SELECT COUNT(*) FROM runtime_settings WHERE key = 'mcp.order_proposals_enabled' AND value <> '0'`); n != 0 {
		t.Fatal("the intake switch was turned on")
	}
}

// The order-proposal tools need the Bearer key like every request: an
// unauthenticated call stores nothing and opens no database connection.
func TestOrderProposalToolsNeedTheKey(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	switchOn(t, w)
	driverName, counter := testkit.NewCountingDriver(t)
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path, DriverName: driverName})
	before := rowCounts(t, w)
	for name, args := range map[string]any{
		"order_proposal_submit":   validProposal("proposal-0001"),
		"order_proposal_get":      map[string]any{"proposal_id": 1},
		"order_proposals_disable": map[string]any{},
	} {
		for _, auth := range []string{"", "Bearer wrong-key", "Basic " + testAPIKey} {
			srv.post(t, auth, callBody(name, args)).wantError(t, http.StatusUnauthorized, paperresultsmcp.CodeUnauthorized)
		}
	}
	if counter.Opens.Load() != 0 || counter.Statements.Load() != 0 {
		t.Fatalf("unauthenticated calls opened %d connections / %d statements", counter.Opens.Load(), counter.Statements.Load())
	}
	wantSameCounts(t, w, before)
}

func stored(t *testing.T, w *sql.DB, key string) (string, bool) {
	t.Helper()
	var v string
	err := w.QueryRowContext(t.Context(), `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return v, true
}
