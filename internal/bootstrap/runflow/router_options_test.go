package runflow_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/router"
)

// TestRouterOptions_WireSecretsStoreIntoSetupGuard drives LoadSecrets and
// RouterOptions the way both entrypoints do: with the required secrets unset,
// the engine's Setup Guard (only active once WithSecretsStore is applied)
// must send operators to /setup.
func TestRouterOptions_WireSecretsStoreIntoSetupGuard(t *testing.T) {
	state, err := bootstrap.Run(bootstrap.Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })

	secretsRepo, secrets, err := bootstrap.LoadSecrets(context.Background(), state)
	if err != nil {
		t.Fatalf("LoadSecrets: %v", err)
	}
	services := bootstrap.BuildServices(state, secrets)

	engine := router.New(bootstrap.RouterOptions(services, state, secretsRepo)...)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/scanner", nil))

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup" {
		t.Fatalf("GET /scanner = %d (Location %q), want 302 to /setup", rec.Code, rec.Header().Get("Location"))
	}
}
