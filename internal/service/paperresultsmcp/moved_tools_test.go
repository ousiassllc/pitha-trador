package paperresultsmcp

import (
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/moved_tools.golden.json from the current table")

// The settings / order-proposal / experiment tools were moved out of the root
// into tool packages (issue #1130) without changing a byte of their contract.
// movedTools pins, per tool, what a client can see in tools/list (description,
// schema, writes, noPeriod) and what the argument decoder answers (spec code and
// detail, or accepted) for a fixed set of arguments. The golden file was taken
// from the code before the move; regenerate with -update-golden only for a
// deliberate contract change.
var movedTools = []string{
	"settings_get", "settings_set", "settings_diff", "settings_audit", "settings_write_enable", "settings_write_disable",
	"order_proposal_submit", "order_proposal_cancel", "order_proposals_disable",
	"order_proposals_list", "order_proposal_get", "order_proposal_summary",
	"experiment_create", "experiment_close", "experiments_list", "experiment_compare",
}

// movedDecodeCases are the raw arguments each tool's decoder is asked about.
var movedDecodeCases = map[string][]string{
	"settings_get": {``, `{}`, `{"x":1}`},
	"settings_set": {`{}`, `{"key":"evil.key","value":1}`, `{"key":"` + mcpwritesettings.Keys()[0] + `","value":true}`, `{"key":"` + mcpwritesettings.Keys()[0] + `","value":"1"}`,
		`{"key":"` + mcpwritesettings.Keys()[0] + `","value":1,"x":1}`, `{"key":"` + mcpwritesettings.Keys()[0] + `","value":-1.5,"hypothesis":"h"}`},
	"settings_diff":          {`{}`, `{"since_id":-1}`, `{"since_id":0}`, `{"since_at":"nope"}`, `{"since_at":"2026-01-02T03:04:05Z"}`, `{"since_id":1,"since_at":"2026-01-02T03:04:05Z"}`},
	"settings_audit":         {`{}`, `{"limit":0}`, `{"limit":1000}`, `{"limit":5,"before_id":0}`, `{"limit":5,"before_id":3}`, `{"limit":"a"}`},
	"settings_write_enable":  {`{}`, `{"x":1}`},
	"settings_write_disable": {`{}`, `{"x":1}`},
	"order_proposal_submit": {`{}`, `{"idempotency_key":"k"}`, `{"x":1}`, `{"idempotency_key":"abcdefgh12345678","symbol":"7203","bucket":"stock","action":"open_long","order_type":"MARKET","rationale":"r","sources":[],"expires_at":"2026-01-02T03:04:05Z"}`,
		`{"idempotency_key":"abcdefgh12345678","symbol":"7203","bucket":"stock","action":"close","order_type":"MARKET","rationale":"r","sources":[{"title":"t","url":"https://example.com/a","retrieved_at":"2026-01-02T03:04:05Z"}],"expires_at":"2026-01-02T03:04:05Z"}`,
		`{"idempotency_key":"abcdefgh12345678","symbol":"7203","bucket":"stock","action":"open_long","quantity":100,"order_type":"LIMIT","limit_price":900,"stop_loss_price":850,"exit_plan":"p","max_loss_jpy":5000,"rationale":"r","sources":[{"title":"t","url":"https://example.com/a","retrieved_at":"2026-01-02T03:04:05Z"}],"expires_at":"2026-01-02T03:04:05Z"}`},
	"order_proposal_cancel":   {`{}`, `{"proposal_id":0}`, `{"proposal_id":7}`, `{"proposal_id":7,"x":1}`},
	"order_proposals_disable": {`{}`, `{"x":1}`},
	"order_proposals_list":    {`{}`, `{"limit":0}`, `{"limit":1000}`, `{"status":"nope"}`, `{"bucket":"nope"}`, `{"bucket":"stock","limit":3}`, `{"from":"2026-01-01"}`},
	"order_proposal_get":      {`{}`, `{"proposal_id":-1}`, `{"proposal_id":7}`},
	"order_proposal_summary":  {`{}`, `{"from":"2026-01-01","to":"2026-01-05"}`, `{"x":1}`},
	"experiment_create": {`{}`, `{"hypothesis":"h","changes":[{"kind":"setting","name":"a","before":true,"after":1}]}`,
		`{"hypothesis":"h","changes":[{"kind":"setting","name":"a","before":"1","after":2}],"audit_id":3}`, `{"hypothesis":"h","changes":[{"kind":"setting","name":"a","before":"1","after":2,"z":1}]}`},
	"experiment_close":   {`{}`, `{"experiment_id":0}`, `{"experiment_id":1,"status":"nope"}`, `{"experiment_id":1,"ended_at":"x"}`, `{"experiment_id":1,"status":"aborted","ended_at":"2026-01-02T03:04:05Z"}`},
	"experiments_list":   {`{}`, `{"limit":0}`, `{"limit":51}`, `{"status":"nope"}`, `{"min_trades":63}`, `{"min_trades":64,"limit":5,"status":"closed"}`},
	"experiment_compare": {`{}`, `{"experiment_id":0}`, `{"experiment_id":1,"min_trades":1}`, `{"experiment_id":1,"min_trades":100}`},
}

type movedDecode struct {
	Args   string `json:"args"`
	Code   string `json:"code,omitempty"`
	Detail string `json:"detail,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
}

type movedEntry struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
	Writes      bool           `json:"writes"`
	NoPeriod    bool           `json:"no_period"`
	Decode      []movedDecode  `json:"decode"`
}

func snapshotMoved(t *testing.T) []movedEntry {
	t.Helper()
	var out []movedEntry
	for _, name := range movedTools {
		tl, ok := findTool(name)
		if !ok {
			t.Fatalf("tool %s is not in the dispatch table", name)
		}
		e := movedEntry{Name: name, Description: tl.description, Schema: tl.schema, Writes: tl.writes, NoPeriod: tl.noPeriod}
		for _, raw := range movedDecodeCases[name] {
			p, _, te := tl.decode(json.RawMessage(raw))
			d := movedDecode{Args: raw}
			if te != nil {
				d.Code, d.Detail = string(te.code), te.detail
			}
			if p.From != nil {
				d.From = *p.From
			}
			if p.To != nil {
				d.To = *p.To
			}
			e.Decode = append(e.Decode, d)
		}
		out = append(out, e)
	}
	return out
}

func TestMovedToolsKeepTheirContract(t *testing.T) {
	got := snapshotMoved(t)
	const path = "testdata/moved_tools.golden.json"
	if *updateGolden {
		var buf []byte
		buf = append(buf, "[\n"...)
		for i, e := range got {
			b, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			buf = append(buf, b...)
			if i < len(got)-1 {
				buf = append(buf, ',')
			}
			buf = append(buf, '\n')
		}
		buf = append(buf, "]\n"...)
		if err := os.WriteFile(path, buf, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want []movedEntry
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) != len(got) {
		t.Fatalf("golden has %d tools, table has %d", len(want), len(got))
	}
	for i := range want {
		gb, _ := json.Marshal(got[i])
		wb, _ := json.Marshal(want[i])
		if string(gb) != string(wb) { // Marshal sorts map keys, so this is a deep comparison
			t.Errorf("%s changed:\n got  %s\n want %s", want[i].Name, gb, wb)
		}
	}
}

// Each moved tool is in the table once, and the table holds no other tool of
// these families.
func TestMovedToolsAreRegisteredOnce(t *testing.T) {
	names := ToolNames()
	for _, n := range movedTools {
		if c := countOf(names, n); c != 1 {
			t.Errorf("%s is in the table %d times, want 1", n, c)
		}
	}
}

func countOf(names []string, n string) int {
	c := 0
	for _, s := range names {
		if s == n {
			c++
		}
	}
	return c
}
