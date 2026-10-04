package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/router"
)

// TestRouterOptions_WireSecretsStoreIntoSetupGuard drives LoadSecrets and
// RouterOptions the way both entrypoints do: with the required secrets unset,
// the engine's Setup Guard (only active once WithSecretsStore is applied)
// must send operators to /setup.
func TestRouterOptions_WireSecretsStoreIntoSetupGuard(t *testing.T) {
	state, err := Run(Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })

	secretsRepo, secrets, err := LoadSecrets(context.Background(), state)
	if err != nil {
		t.Fatalf("LoadSecrets: %v", err)
	}
	services, err := BuildServices(state, secrets, nil)
	if err != nil {
		t.Fatalf("BuildServices: %v", err)
	}

	engine := router.New(RouterOptions(services, state, secretsRepo)...)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/scanner", nil))

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup" {
		t.Fatalf("GET /scanner = %d (Location %q), want 302 to /setup", rec.Code, rec.Header().Get("Location"))
	}
}
