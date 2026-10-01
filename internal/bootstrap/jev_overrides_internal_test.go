package bootstrap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
)

// Issues #271/#274: the stored JEV_BASE_URL/JEV_MODEL secrets reach the Jev
// client BuildServices constructs, and an unset JEV_MODEL sends "jev-latest".
func TestBuildServices_JevBaseURLAndModelComeFromSecrets(t *testing.T) {
	for _, tc := range []struct {
		name, model, wantModel string
	}{
		{"stored model overrides default", "jev-custom", "jev-custom"},
		{"unset model falls back to jev-latest", "", "jev-latest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotModel string
			scout := jevtest.ScoutHandler(jev.ScoutResponse{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				var body struct {
					Model string `json:"model"`
				}
				_ = json.Unmarshal(raw, &body)
				gotModel = body.Model
				scout(w, r)
			}))
			t.Cleanup(server.Close)

			svc := buildServicesWithSecrets(t, config.Secrets{JevBaseURL: server.URL, JevModel: tc.model})
			if _, _, err := svc.Jev.Scout(context.Background(), jev.ScoutRequest{}); err != nil {
				t.Fatalf("Jev.Scout: %v", err)
			}
			if gotModel != tc.wantModel {
				t.Errorf("model sent = %q, want %q", gotModel, tc.wantModel)
			}
		})
	}
}
