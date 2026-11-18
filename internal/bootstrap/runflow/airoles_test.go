package runflow_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/airole"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
)

type seen struct {
	mu                sync.Mutex
	path, auth, model string
	requests          int
}

func (s *seen) handler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.path, s.auth, s.model = r.URL.Path, r.Header.Get("Authorization"), body.Model
		s.requests++
		s.mu.Unlock()
		next(w, r)
	}
}

// The live Jev BuildServices builds: with no saved role setting it calls
// JEV_BASE_URL with the JEV key and JEV_MODEL as before; with the Jev role saved
// as OpenRouter (the Settings screen's save) it calls OpenRouter with the
// OpenRouter key and the role's model, and never touches the 独自 API.
func TestBuildServices_LiveJevFollowsItsRoleSetting(t *testing.T) {
	for _, tc := range []struct {
		name                string
		role                *airole.Setting
		wantOR              bool
		wantAuth, wantModel string
	}{
		{"default: the 独自 API as before", nil, false, "Bearer fake-jev-key", "jev-custom"},
		{"OpenRouter: its key and the role's model", &airole.Setting{Provider: airole.ProviderOpenRouter, Model: "vendor/jev"}, true, "Bearer fake-or-key", "vendor/jev"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var custom, or seen
			customSrv := httptest.NewServer(custom.handler(jevtest.ScoutHandler(jev.ScoutResponse{})))
			orSrv := httptest.NewServer(or.handler(jevtest.ScoutHandler(jev.ScoutResponse{})))
			t.Cleanup(customSrv.Close)
			t.Cleanup(orSrv.Close)

			state, err := bootstrap.Run(bootstrap.Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = state.Close() })
			if tc.role != nil {
				if err := system.NewAIRoleRepository(state.DB).Save(context.Background(), airole.RoleJev, *tc.role); err != nil {
					t.Fatal(err)
				}
			}
			svc := bootstrap.BuildServices(state, config.Secrets{
				JevAPIKey: "fake-jev-key", JevBaseURL: customSrv.URL, JevModel: "jev-custom",
				OpenRouterAPIKey: "fake-or-key", OpenRouterBaseURL: orSrv.URL,
			})
			if _, _, err := svc.Jev.Scout(context.Background(), jev.ScoutRequest{}); err != nil {
				t.Fatalf("Scout: %v", err)
			}
			hit, other := &custom, &or
			if tc.wantOR {
				hit, other = &or, &custom
			}
			if hit.requests != 1 || hit.path != jev.Endpoint || hit.auth != tc.wantAuth || hit.model != tc.wantModel {
				t.Errorf("request = path %q auth %q model %q (%d requests), want path %s auth %q model %q once", hit.path, hit.auth, hit.model, hit.requests, jev.Endpoint, tc.wantAuth, tc.wantModel)
			}
			if other.requests != 0 {
				t.Errorf("the other server got %d requests", other.requests)
			}
		})
	}
}
