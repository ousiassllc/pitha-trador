package promotiontests

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// The tool is registered read-only with exactly the declared arguments (and nothing that could apply a promotion).
func TestToolIsRegisteredReadOnly(t *testing.T) {
	t.Parallel()
	srv, _ := newServer(t, "")
	r := srv.postRaw(t, "tools/list")
	var env struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				Annotations map[string]any `json:"annotations"`
				InputSchema struct {
					Properties           map[string]any `json:"properties"`
					Required             []string       `json:"required"`
					AdditionalProperties bool           `json:"additionalProperties"`
				} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil || r.status != http.StatusOK {
		t.Fatalf("tools/list: %d %s %v", r.status, r.body, err)
	}
	found := false
	for _, tl := range env.Result.Tools {
		if tl.Name != tool {
			continue
		}
		found = true
		if tl.Annotations["readOnlyHint"] != true || tl.Annotations["destructiveHint"] != false || tl.InputSchema.AdditionalProperties {
			t.Errorf("annotations %v, schema %+v", tl.Annotations, tl.InputSchema)
		}
		var props []string
		for p := range tl.InputSchema.Properties {
			props = append(props, p)
		}
		slices.Sort(props)
		want := "candidate,current,family_size_extra,from,horizon,max_adjusted_p,min_common_rows,min_days,min_diff_ci_lower_bps,min_positive_day_pct,notional_jpy,to,usd_jpy"
		if got := strings.Join(props, ","); got != want {
			t.Errorf("arguments = %s", got)
		}
	}
	if !found {
		t.Fatalf("%s is not in tools/list", tool)
	}
}
