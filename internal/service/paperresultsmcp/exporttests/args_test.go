package exporttests

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// The tool is the one research tool that writes: it is registered not read-only and not destructive,
// and its arguments are the period and the split only: no path.
func TestRegistrationAndArguments(t *testing.T) {
	srv, _ := newServer(t, "")
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
	found := false
	for _, tl := range env.Result.Tools {
		if tl.Name != tool {
			continue
		}
		found = true
		if tl.Annotations["readOnlyHint"] != false || tl.Annotations["destructiveHint"] != false || tl.InputSchema.AdditionalProperties {
			t.Errorf("annotations %v, schema %+v", tl.Annotations, tl.InputSchema)
		}
		var props []string
		for p := range tl.InputSchema.Properties {
			props = append(props, p)
		}
		slices.Sort(props)
		if !slices.Equal(props, []string{"embargo_days", "from", "split_at", "to"}) {
			t.Errorf("arguments = %v", props)
		}
	}
	if !found {
		t.Fatalf("%s is not in tools/list", tool)
	}
}

// A bad call is an invalid_argument (or invalid_period) before anything is read or written, switch on or off:
// an unknown field (in particular any attempt to name a path), a malformed or misplaced split_at, an embargo
// out of 0..30, an embargo without a split, a split without the period spelled out.
func TestBadArgumentsAreRefusedAndWriteNothing(t *testing.T) {
	srv, w := newServer(t, "")
	seed(t, w, fixture())
	enableExport(t, w)
	bad := map[string]map[string]any{
		"path":                  {"path": "/tmp/elsewhere"},
		"dir":                   {"dir": "/tmp/elsewhere"},
		"save_dir":              {"save_dir": "../.."},
		"output":                {"output": "x"},
		"label_horizons":        {"label_horizons": []int{10}},
		"split_at not a date":   {"split_at": "2026-13-45"},
		"split_at wrong format": {"split_at": "20261008"},
		"split_at not a string": {"split_at": 20261008},
		"split_at = from":       {"split_at": periodFrom},
		"split_at = to":         {"split_at": periodTo},
		"split_at before from":  {"split_at": "2026-10-01"},
		"split_at after to":     {"split_at": "2026-10-20"},
		"embargo negative":      {"split_at": "2026-10-08", "embargo_days": -1},
		"embargo over 30":       {"split_at": "2026-10-08", "embargo_days": 31},
		"embargo fractional":    {"split_at": "2026-10-08", "embargo_days": 1.5},
		"embargo without split": {"embargo_days": 2},
		"embargo empties eval":  {"split_at": "2026-10-08", "embargo_days": 4},
	}
	for name, args := range bad {
		t.Run(name, func(t *testing.T) {
			srv.call(t, args).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
		})
	}
	t.Run("split without a period", func(t *testing.T) {
		r := srv.post(t, "tools/call", map[string]any{"name": tool, "arguments": map[string]any{"split_at": "2026-10-08"}})
		r.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	})
	t.Run("period errors keep their own code", func(t *testing.T) {
		srv.call(t, map[string]any{"from": "2026-10-05", "to": "2026-10-05"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidPeriod)
	})
	if entries := listDir(t, srv.dbDir); slices.ContainsFunc(entries, func(e string) bool { return e == "exports" }) {
		t.Errorf("a refused call created the exports folder: %v", entries)
	}
	// The same refusals while the switch is off.
	testkit.MustExec(t, w, `DELETE FROM runtime_settings WHERE key = 'research.export.enabled'`)
	srv.call(t, map[string]any{"path": "/tmp/x"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
}

// The boundary values of the split are accepted: the day after from, the last day of the period, an
// embargo of 0 and of the most that still leaves an eval day.
func TestSplitBoundariesAreAccepted(t *testing.T) {
	srv, w := newServer(t, "")
	seed(t, w, fixture())
	enableExport(t, w)
	for _, args := range []map[string]any{
		{"split_at": "2026-10-06"},
		{"split_at": "2026-10-11"},
		{"split_at": "2026-10-08", "embargo_days": 0},
		{"split_at": "2026-10-08", "embargo_days": 3},
	} {
		resp := srv.call(t, args).export(t)
		if !resp.Written || resp.Manifest == nil || resp.Manifest.RowsTrain+resp.Manifest.RowsEval+resp.Manifest.RowsInEmbargo+resp.Manifest.RowsPurged != resp.Manifest.Decisions {
			t.Errorf("%v: %+v", args, resp)
		}
	}
}
