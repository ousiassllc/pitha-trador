package runflow_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
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

// The selection saved in Settings is what the next start reads: BuildServices
// builds the 立花 adapter (issue #724), and a selection that is not usable yet
// (no 秘密鍵 path saved) comes up as a failed session with guidance, never as a
// build failure.
func TestLoadBrokerSettings_ReadsSavedSelectionAndBuildsTachibana(t *testing.T) {
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
	svc := bootstrap.BuildServices(state, secrets, bootstrap.WithBrokerSettings(got))
	if svc.Broker == nil || svc.Broker.Capabilities().Name != config.BrokerTachibana {
		t.Fatalf("broker = %+v, want the tachibana adapter", svc.Broker)
	}
	if caps := svc.Broker.Capabilities(); caps.Ranking || caps.MaxStreamSymbols != 120 {
		t.Errorf("capabilities = %+v, want no ranking and 120 stream symbols", caps)
	}

	runCtx, cancel := context.WithCancel(ctx)
	err = svc.Broker.Start(runCtx)
	cancel()
	svc.Broker.Run(runCtx) // returns once the session loop has stopped
	if err == nil {
		t.Fatal("Start without a 秘密鍵 must report a failed session")
	}
	if st := svc.Broker.Status(); st.Issue != broker.SessionIssueKeyMismatch || st.Guidance == "" {
		t.Errorf("status = %+v, want key_mismatch with guidance", st)
	}
}
