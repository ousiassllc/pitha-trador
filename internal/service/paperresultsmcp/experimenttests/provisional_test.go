package experimenttests

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// 暫定値の表示: the tool descriptions an MCP client reads say that the floor
// is provisional and that the production sigma is not measured yet (the wording
// is not pinned beyond those two facts).
func TestToolDescriptionsStateTheFloorIsProvisional(t *testing.T) {
	srv, _ := newServer(t)
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	req, err := http.NewRequest(http.MethodPost, srv.http.URL, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var out struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				InputSchema struct {
					Properties map[string]struct {
						Description string `json:"description"`
					} `json:"properties"`
				} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, tool := range out.Result.Tools {
		if tool.Name != "experiments_list" && tool.Name != "experiment_compare" {
			continue
		}
		seen++
		for where, text := range map[string]string{"description": tool.Description, "min_trades": tool.InputSchema.Properties["min_trades"].Description} {
			if !strings.Contains(text, "暫定") || !strings.Contains(text, "σ は未確認") {
				t.Errorf("%s %s does not say the floor is provisional and sigma unmeasured: %s", tool.Name, where, text)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("found %d of the 2 experiment read tools", seen)
	}
}
