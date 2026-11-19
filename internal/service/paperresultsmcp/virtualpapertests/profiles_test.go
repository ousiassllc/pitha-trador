package virtualpapertests

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

type createResult struct {
	Profile profileView `json:"profile"`
}

func (s *testServer) create(t *testing.T, args map[string]any) profileView {
	t.Helper()
	var out createResult
	s.call(t, "virtual_paper_profile_create", args).data(t, "virtual_paper_profile_create", &out)
	return out.Profile
}

func TestCreateCloneAndStop(t *testing.T) {
	srv, w := newServer(t)
	a := srv.create(t, map[string]any{"name": "early_tp", "params": map[string]any{"take_profit_pct": 1, "min_entry_quality_long": "good"}})
	if a.Name != "early_tp" || a.Status != "active" || a.Params["take_profit_pct"] != float64(1) || a.FirstDay != "2026-10-19" {
		t.Fatalf("created = %+v, want an active profile whose first day is the day after the (fixed) today", a)
	}
	b := srv.create(t, map[string]any{"name": "clone", "clone_from": a.ID, "params": map[string]any{"take_profit_pct": 3, "fee_bps": 2}})
	if b.ClonedFrom == nil || *b.ClonedFrom != a.ID || b.Params["take_profit_pct"] != float64(3) || b.Params["min_entry_quality_long"] != "good" || b.Params["fee_bps"] != float64(2) {
		t.Fatalf("clone = %+v, want a's params overridden by the given ones", b)
	}

	var stop struct {
		Profile        profileView `json:"profile"`
		Stopped        bool        `json:"stopped"`
		AlreadyStopped bool        `json:"already_stopped"`
	}
	srv.call(t, "virtual_paper_profile_stop", map[string]any{"profile_id": a.ID}).data(t, "virtual_paper_profile_stop", &stop)
	if !stop.Stopped || stop.AlreadyStopped || stop.Profile.Status != "stopped" {
		t.Fatalf("stop = %+v", stop)
	}
	stop.Stopped, stop.AlreadyStopped = false, false
	srv.call(t, "virtual_paper_profile_stop", map[string]any{"profile_id": a.ID}).data(t, "virtual_paper_profile_stop", &stop)
	if stop.Stopped || !stop.AlreadyStopped {
		t.Fatalf("stopping twice = %+v, want already_stopped and no error", stop)
	}
	srv.call(t, "virtual_paper_profile_stop", map[string]any{"profile_id": 999}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)

	if n := count(t, w, `SELECT COUNT(*) FROM virtual_paper_profiles`); n != 2 {
		t.Errorf("%d profiles stored, want 2", n)
	}
	var actor, client string
	if err := w.QueryRowContext(t.Context(), `SELECT actor, client FROM virtual_paper_profiles WHERE id = ?`, a.ID).Scan(&actor, &client); err != nil || actor != "mcp" || client == testAPIKey {
		t.Errorf("actor %q client %q (%v)", actor, client, err)
	}
	// The record is research-only: no order, position, signal, setting or audit row.
	for _, tbl := range []string{"paper_orders", "positions", "trade_signals", "virtual_paper_trades", "virtual_paper_days", "mcp_settings_audit"} {
		if n := count(t, w, `SELECT COUNT(*) FROM `+tbl); n != 0 {
			t.Errorf("%s has %d rows after profile writes, want 0", tbl, n)
		}
	}
}

func TestCreateRefusals(t *testing.T) {
	srv, w := newServer(t)
	srv.create(t, map[string]any{"name": "taken"})
	unprocessable := http.StatusUnprocessableEntity
	cases := []struct {
		name string
		args map[string]any
		code paperresultsmcp.Code
		want string // a substring of error.data.detail
	}{
		{"range", map[string]any{"name": "x", "params": map[string]any{"take_profit_pct": 500}}, paperresultsmcp.CodeInvalidValue, "take_profit_pct"},
		{"enum", map[string]any{"name": "x", "params": map[string]any{"entry_rule": "nope"}}, paperresultsmcp.CodeInvalidValue, "entry_rule"},
		{"duplicate", map[string]any{"name": "taken"}, paperresultsmcp.CodeInvalidValue, "name"},
		{"empty name", map[string]any{"name": ""}, paperresultsmcp.CodeInvalidValue, "name"},
		{"long name", map[string]any{"name": strings.Repeat("a", 41)}, paperresultsmcp.CodeInvalidValue, "name"},
		{"unknown param", map[string]any{"name": "x", "params": map[string]any{"leverage": 3}}, paperresultsmcp.CodeInvalidArgument, ""},
		{"unknown clone", map[string]any{"name": "x", "clone_from": 999}, paperresultsmcp.CodeInvalidArgument, "clone_from"},
		{"missing name", map[string]any{}, paperresultsmcp.CodeInvalidArgument, ""},
	}
	for _, c := range cases {
		detail := srv.call(t, "virtual_paper_profile_create", c.args).wantError(t, unprocessable, c.code)
		if !strings.Contains(detail, c.want) {
			t.Errorf("%s: detail %q does not name %q", c.name, detail, c.want)
		}
	}
	if n := count(t, w, `SELECT COUNT(*) FROM virtual_paper_profiles`); n != 1 {
		t.Errorf("%d profiles after refusals, want only the first", n)
	}
}

func TestCaps(t *testing.T) {
	srv, w := newServer(t)
	var ids []int64
	for i := range 5 {
		ids = append(ids, srv.create(t, map[string]any{"name": fmt.Sprintf("p%d", i)}).ID)
	}
	detail := srv.call(t, "virtual_paper_profile_create", map[string]any{"name": "sixth"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
	if !strings.Contains(detail, "5") {
		t.Errorf("detail %q does not name the active cap", detail)
	}
	for i := 5; i < 20; i++ { // stopped ones still count in the total of 20
		id := srv.create2(t, ids[0], fmt.Sprintf("p%d", i))
		srv.call(t, "virtual_paper_profile_stop", map[string]any{"profile_id": id})
	}
	detail = srv.call(t, "virtual_paper_profile_create", map[string]any{"name": "twenty_first"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeLimitExceeded)
	if !strings.Contains(detail, "20") {
		t.Errorf("detail %q does not name the total cap", detail)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM virtual_paper_profiles`); n != 20 {
		t.Errorf("%d profiles, want 20", n)
	}
}

// create2 stops a slot first (the cap on active profiles), then creates.
func (s *testServer) create2(t *testing.T, stopFirst int64, name string) int64 {
	t.Helper()
	s.call(t, "virtual_paper_profile_stop", map[string]any{"profile_id": stopFirst})
	return s.create(t, map[string]any{"name": name}).ID
}

// While the write switch is off both write tools are refused with write_disabled
// and store nothing; the comparison (a read) still answers.
func TestWriteSwitchOff(t *testing.T) {
	srv, w := newServer(t)
	p := srv.create(t, map[string]any{"name": "p"})
	testkit.MustExec(t, w, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('mcp.settings_write_enabled', '0', 't')`)

	srv.call(t, "virtual_paper_profile_create", map[string]any{"name": "q"}).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	srv.call(t, "virtual_paper_profile_stop", map[string]any{"profile_id": p.ID}).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	if n := count(t, w, `SELECT COUNT(*) FROM virtual_paper_profiles WHERE name = 'q' OR status = 'stopped'`); n != 0 {
		t.Errorf("%d rows were written while the switch was off", n)
	}
	var out struct {
		Profiles []any `json:"profiles"`
	}
	srv.call(t, "virtual_paper_compare", map[string]any{}).data(t, "virtual_paper_compare", &out)
}

func TestToolListAndNoSecrets(t *testing.T) {
	srv, _ := newServer(t)
	res := srv.post(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	var list struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				Annotations map[string]any `json:"annotations"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := jsonUnmarshal(res.body, &list); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"virtual_paper_profile_create": false, "virtual_paper_profile_stop": false, "virtual_paper_compare": true}
	for _, tl := range list.Result.Tools {
		if ro, ok := want[tl.Name]; ok {
			if tl.Annotations["readOnlyHint"] != ro {
				t.Errorf("%s readOnlyHint = %v, want %v", tl.Name, tl.Annotations["readOnlyHint"], ro)
			}
			delete(want, tl.Name)
		}
	}
	if len(want) != 0 {
		t.Errorf("tools missing from tools/list: %v", want)
	}
	srv.create(t, map[string]any{"name": "p"})
	for _, body := range [][]byte{res.body, srv.call(t, "virtual_paper_compare", map[string]any{}).body} {
		if strings.Contains(string(body), testAPIKey) {
			t.Error("the API key appears in a response")
		}
	}
	if strings.Contains(srv.logs.String(), testAPIKey) {
		t.Error("the API key appears in the log")
	}
}
