package proposaltests

import (
	"net/http"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
)

// The Settings screen's switch (mcpset.Gate, what POST /settings/mcp/order-proposals
// calls) is the only way on: it stores the value the MCP process then reads, and
// audits it as a GUI change. The MCP then switches it off again, and the GUI
// sees that.
func TestGUITurnsTheIntakeOnAndTheMCPTurnsItOff(t *testing.T) {
	srv, w := proposalServer(t)
	gate := mcpset.NewGate(w)
	ctx := t.Context()

	if on, err := gate.OrderProposalsEnabled(ctx); err != nil || on {
		t.Fatalf("initial state: on=%v err=%v, want off", on, err)
	}
	srv.call(t, "order_proposal_submit", validProposal("proposal-0001")).wantError(t, http.StatusForbidden, paperresultsmcp.CodeProposalsDisabled)

	if err := gate.SetOrderProposalsEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if on, err := gate.OrderProposalsEnabled(ctx); err != nil || !on {
		t.Fatalf("after the GUI turned it on: on=%v err=%v", on, err)
	}
	if got := submit(t, srv, validProposal("proposal-0001")); got.Status != "received" {
		t.Fatalf("submit after the GUI turned the intake on: %+v", got)
	}
	// The write switch is a separate value: the GUI's intake switch left it alone.
	if _, ok := stored(t, w, "mcp.settings_write_enabled"); ok {
		t.Error("the intake switch wrote the write switch")
	}

	srv.call(t, "order_proposals_disable", map[string]any{}).data(t, "order_proposals_disable", &struct{}{})
	if on, err := gate.OrderProposalsEnabled(ctx); err != nil || on {
		t.Fatalf("after the MCP turned it off: on=%v err=%v", on, err)
	}

	entries, err := gate.Recent(ctx, 10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("audit rows = %d, err %v", len(entries), err)
	}
	disable, enable := entries[0], entries[1]
	if disable.Actor != mcpset.ActorMCP || disable.Action != mcpset.ActionOrderProposalsDisable || disable.Client != "127.0.0.1" ||
		enable.Actor != mcpset.ActorGUI || enable.Action != mcpset.ActionOrderProposalsEnable || enable.Client != "" ||
		enable.Key != "mcp.order_proposals_enabled" || enable.Before == nil || *enable.Before != "0" || enable.After == nil || *enable.After != "1" {
		t.Errorf("audit rows: %+v / %+v", disable, enable)
	}
}
