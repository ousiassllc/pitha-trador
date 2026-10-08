package runflow_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
)

func newState(t *testing.T) *bootstrap.State {
	t.Helper()
	state, err := bootstrap.Run(bootstrap.Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	return state
}

// Nothing stored (a fresh install, or an upgrade from before issue #733)
// starts exactly as before: the kabu adapter.
func TestLoadBrokerSettings_DefaultsToKabu(t *testing.T) {
	state := newState(t)
	got, err := bootstrap.LoadBrokerSettings(context.Background(), state)
	if err != nil {
		t.Fatalf("LoadBrokerSettings: %v", err)
	}
	if got.Provider != config.BrokerKabu || got.Tachibana.Environment != config.TachibanaEnvDemo {
		t.Errorf("defaults = %+v, want kabu / demo", got)
	}
	_, secrets, err := bootstrap.LoadSecrets(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if name := bootstrap.BuildServices(state, secrets, bootstrap.WithBrokerSettings(got)).Broker.Capabilities().Name; name != config.BrokerKabu {
		t.Errorf("broker = %q, want kabu", name)
	}
}

// The selection saved in Settings is what the next start reads. The tachibana
// adapter does not exist yet (issue #724), so BuildServices must still come up
// - on kabu - instead of failing on the saved selection.
func TestLoadBrokerSettings_ReadsSavedSelectionAndBuildStillComesUp(t *testing.T) {
	state := newState(t)
	repo := system.NewRuntimeSettingsRepository(state.DB)
	ctx := context.Background()
	for key, value := range map[string]string{
		config.KeyBrokerProvider:       `"tachibana"`,
		config.KeyTachibanaEnvironment: `"production"`,
	} {
		if err := repo.Set(ctx, key, value, time.Now()); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}

	got, err := bootstrap.LoadBrokerSettings(ctx, state)
	if err != nil {
		t.Fatalf("LoadBrokerSettings: %v", err)
	}
	if got.Provider != config.BrokerTachibana || !got.Tachibana.Production() {
		t.Fatalf("settings = %+v, want tachibana / production", got)
	}
	_, secrets, err := bootstrap.LoadSecrets(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if svc := bootstrap.BuildServices(state, secrets, bootstrap.WithBrokerSettings(got)); svc.Broker == nil {
		t.Fatal("BuildServices produced no broker")
	}
}
