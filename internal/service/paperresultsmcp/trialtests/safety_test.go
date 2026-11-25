package trialtests

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

var trialTools = []string{"jev_trial_compare", "jev_trial_cost"}

// forbiddenKeyWords must appear in no key of any output: the tools hold no vote, consensus, winner,
// recommendation or promotion.
var forbiddenKeyWords = []string{"winner", "recommend", "adopt", "promot", "majority", "consensus", "vote", "best", "champion", "select"}

func walkKeys(v any, visit func(key string)) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			visit(k)
			walkKeys(child, visit)
		}
	case []any:
		for _, child := range x {
			walkKeys(child, visit)
		}
	}
}

// Both tools are registered read-only (annotation), take only the declared arguments, and reach
// no write: the runner's stop, failure, throttle and login records are not on the MCP's settings whitelist.
func TestTrialToolsAreReadOnlyAndTheRunnerStateIsNotWritable(t *testing.T) {
	srv, w := newServer(t)
	var env struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				Annotations map[string]any `json:"annotations"`
				InputSchema struct {
					Properties           map[string]any `json:"properties"`
					AdditionalProperties bool           `json:"additionalProperties"`
				} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	r := srv.post(t, "tools/list", nil)
	if err := json.Unmarshal(r.body, &env); err != nil || r.status != http.StatusOK {
		t.Fatalf("tools/list: %d %s %v", r.status, r.body, err)
	}
	found := 0
	for _, tl := range env.Result.Tools {
		if !slices.Contains(trialTools, tl.Name) {
			continue
		}
		found++
		if tl.Annotations["readOnlyHint"] != true || tl.Annotations["destructiveHint"] != false || tl.InputSchema.AdditionalProperties {
			t.Errorf("%s: annotations %v, schema %+v", tl.Name, tl.Annotations, tl.InputSchema)
		}
		for _, p := range []string{"from", "to", "arms"} {
			if _, ok := tl.InputSchema.Properties[p]; !ok {
				t.Errorf("%s: no %s argument", tl.Name, p)
			}
		}
		if len(tl.InputSchema.Properties) != 3 {
			t.Errorf("%s: arguments %v, want from/to/arms only", tl.Name, tl.InputSchema.Properties)
		}
	}
	if found != 2 {
		t.Fatalf("found %d trial tools in tools/list, want 2", found)
	}

	// Only the six operator keys (issue #1220) are on the write whitelist; the runner's
	// own state is not, and settings_set refuses it without storing anything.
	runnerState := []string{
		jevtrialsettings.KeyHalt, jevtrialsettings.KeyCodexHalt, jevtrialsettings.KeyCodexLogin,
		jevtrialsettings.HaltKey(armA), jevtrialsettings.FailuresKey(armA), jevtrialsettings.ThrottleKey(armA),
	}
	for _, k := range jevtrialsettings.Keys() {
		if !slices.Contains(jevtrialsettings.OperatorKeys(), k) && !slices.Contains(runnerState, k) {
			t.Errorf("%s is neither an operator key nor runner state: classify it", k)
		}
	}
	for _, k := range runnerState {
		if mcpwritesettings.IsAllowed(k) {
			t.Errorf("%s is writable from the MCP", k)
		}
		srv.call(t, "settings_set", map[string]any{"key": k, "value": 1}).wantError(t, http.StatusForbidden, paperresultsmcp.CodeKeyNotAllowed)
	}
	var n int
	if err := w.QueryRow(`SELECT COUNT(*) FROM runtime_settings WHERE key LIKE 'jev.trial.%'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("trial settings rows after settings_set = %d, %v", n, err)
	}
	// The tools write nothing either.
	seedMain(t, w)
	before := count(t, w, `SELECT COUNT(*) FROM jev_trial_decisions`)
	for _, name := range trialTools {
		srv.call(t, name, period)
	}
	if after := count(t, w, `SELECT COUNT(*) FROM jev_trial_decisions`); after != before {
		t.Errorf("rows %d -> %d", before, after)
	}
}

func count(t *testing.T, w *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := w.QueryRowContext(t.Context(), q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Nothing in either output names a vote, consensus, winner, recommendation or promotion, and
// neither carries a response body, an error excerpt, a key or a setting's raw value.
func TestTrialOutputsHaveNoVerdictFieldsAndNoSecrets(t *testing.T) {
	srv, w := newServer(t)
	seedMain(t, w)
	seedCost(t, w) // cost rows and the trial's settings alongside the comparison's rows
	for _, name := range trialTools {
		for _, args := range []map[string]any{period, {}, withArms(armA, armB, armC)} {
			r := srv.call(t, name, args)
			var env struct {
				Result struct {
					StructuredContent struct {
						Data any `json:"data"`
					} `json:"structuredContent"`
				} `json:"result"`
			}
			if err := json.Unmarshal(r.body, &env); err != nil || r.status != http.StatusOK {
				t.Fatalf("%s: %d %s %v", name, r.status, r.body, err)
			}
			walkKeys(env.Result.StructuredContent.Data, func(key string) {
				for _, bad := range forbiddenKeyWords {
					if strings.Contains(strings.ToLower(key), bad) {
						t.Errorf("%s: output key %q names a verdict (%s)", name, key, bad)
					}
				}
			})
			body := string(r.body)
			for _, secret := range []string{sentinelBody, sentinelError, testAPIKey, "response_json", "error_excerpt", "OPENROUTER", "sk-", "api_key"} {
				if strings.Contains(body, secret) {
					t.Errorf("%s: output contains %q", name, secret)
				}
			}
			for _, s := range testkit.Sentinels() {
				if strings.Contains(body, s) {
					t.Errorf("%s: output contains sentinel %s", name, s)
				}
			}
		}
	}
}

// Bad arguments are invalid_argument (never partial output): unknown fields, a bad or repeated arm id,
// too many arms, a wrong type; a bad period is invalid_period like every period tool.
func TestTrialToolsRejectBadArguments(t *testing.T) {
	srv, _ := newServer(t)
	many := []string{}
	for i := range jevtrialsettings.MaxArms + 1 {
		many = append(many, "arm-"+strings.Repeat("x", i+1))
	}
	for _, name := range trialTools {
		for _, a := range []map[string]any{
			{"unknown": 1}, {"arms": "arm-a"}, {"arms": []string{"Arm-A"}}, {"arms": []string{""}}, {"arms": []string{armA, armA}},
			{"arms": []string{"has space"}}, {"arms": []string{strings.Repeat("a", 49)}}, {"arms": many}, {"arms": []int{1}},
		} {
			srv.call(t, name, a).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
		}
		srv.call(t, name, map[string]any{"from": "2026-10-01"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidPeriod)
		srv.call(t, name, map[string]any{"from": "2026-10-10", "to": "2026-10-01"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidPeriod)
	}
}
