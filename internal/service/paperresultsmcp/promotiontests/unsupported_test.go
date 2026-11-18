package promotiontests

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

// detailOf is error.data.detail of an error response ("" when there is none).
func detailOf(t *testing.T, r response) string {
	t.Helper()
	var e struct {
		Error struct {
			Data struct {
				Detail string `json:"detail"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.body, &e); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
	return e.Error.Data.Detail
}

// A kind or a combination the checklist cannot check is unsupported_candidate_kind (422), apart from the
// invalid_argument (422) of every other bad argument; it applies nothing and the response carries a fixed
// reason, never what the caller sent.
func TestUnsupportedCandidateKindIsADistinctCode(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	seed(t, w)
	cand := map[string]any{"kind": "shadow", "id": "trader-v9"}
	secret := "SECRET-VALUE-42"
	unsupported := map[string]map[string]any{
		"experiment as current":     {"candidate": cand, "current": map[string]any{"kind": "experiment", "id": "7"}},
		"setting as current":        {"candidate": cand, "current": map[string]any{"kind": "setting", "id": secret}},
		"live_cohort as current":    {"candidate": cand, "current": map[string]any{"kind": "live_cohort", "id": "m/trader-v2"}},
		"setting not on whitelist":  {"candidate": map[string]any{"kind": "setting", "id": secret}},
		"experiment with a current": {"candidate": map[string]any{"kind": "experiment", "id": "7"}, "current": map[string]any{"kind": "baseline", "id": "none"}},
		"setting with a current":    {"candidate": map[string]any{"kind": "setting", "id": "risk.paper.max_trade_loss_pct"}, "current": map[string]any{"kind": "live", "id": "live"}},
	}
	before := dump(t, w)
	for name, args := range unsupported {
		r := srv.post(t, tool, args)
		r.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeUnsupportedCandidateKind)
		if d := detailOf(t, r); d == "" || strings.Contains(string(r.body), secret) {
			t.Errorf("%s: detail %q (body %s), want a fixed reason that does not quote the input", name, d, r.body)
		}
	}
	invalid := map[string]map[string]any{
		"unknown field":      {"candidate": cand, "x": 1},
		"unknown kind":       {"candidate": map[string]any{"kind": "oracle", "id": "x"}},
		"no candidate":       {},
		"same as current":    {"candidate": map[string]any{"kind": "baseline", "id": "none"}, "current": map[string]any{"kind": "baseline", "id": "none"}},
		"experiment bad id":  {"candidate": map[string]any{"kind": "experiment", "id": "seven"}},
		"experiment horizon": {"candidate": map[string]any{"kind": "experiment", "id": "7"}, "horizon": "30m"},
		"experiment period":  withPeriod(map[string]any{"candidate": map[string]any{"kind": "experiment", "id": "7"}}),
	}
	for name, args := range invalid {
		r := srv.post(t, tool, args)
		r.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
		if d := detailOf(t, r); d != "" {
			t.Errorf("%s: invalid_argument carries the detail %q", name, d)
		}
	}
	if after := dump(t, w); len(after) != len(before) {
		t.Fatalf("tables changed: %d -> %d", len(before), len(after))
	} else {
		for table, h := range before {
			if after[table] != h {
				t.Errorf("table %s changed", table)
			}
		}
	}
}

// The error table and the API chapter name the code, its status and its fixed message, as the server answers it.
func TestUnsupportedCandidateKindIsDocumented(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	seed(t, w)
	r := srv.post(t, tool, map[string]any{"candidate": map[string]any{"kind": "setting", "id": "llm.api_key"}})
	r.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeUnsupportedCandidateKind)
	var e struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(r.body, &e); err != nil || e.Message == "" {
		t.Fatalf("message of %s: %v", r.body, err)
	}
	for file, parts := range map[string][]string{
		"architecture/paper-results-mcp-errors.md":                    {"| `unsupported_candidate_kind` | 422 |", e.Message},
		"api/endpoints/research-tools/paper-results-mcp-promotion.md": {"`unsupported_candidate_kind`", "422"},
		"architecture/research-tools/paper-results-mcp-promotion.md":  {"unsupported_candidate_kind", "## 6."},
	} {
		raw, err := os.ReadFile("../../../../docs/" + file)
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range parts {
			if !strings.Contains(string(raw), part) {
				t.Errorf("docs/%s must contain %q", file, part)
			}
		}
	}
}
