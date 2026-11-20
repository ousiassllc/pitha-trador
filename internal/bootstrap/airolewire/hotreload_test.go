package airolewire_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/airolewire"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/airole"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/airoles"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// hotEnv is the app as the composition root builds it (issue #1112): real settings and
// secrets stores, the Settings service that saves a role, and Sol, Opus and Luna built
// once at start-up from the Roles. The AI servers are mocks; the keys are fakes.
type hotEnv struct {
	srv      servers
	settings *system.RuntimeSettingsRepository
	secrets  *system.SecretsRepository
	roles    airolewire.Roles
	svc      *airoles.Service
	live     *jev.Client
	sol      *assist.Sol
	opus     *assist.Opus
	luna     *assist.Luna
}

func newHotEnv(t *testing.T) *hotEnv {
	t.Helper()
	srv, secrets := newServers(t)
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	e := &hotEnv{srv: srv, settings: system.NewRuntimeSettingsRepository(db), secrets: system.NewSecretsRepository(db)}
	if err := e.secrets.Set(context.Background(), config.KeyOpenRouterAPIKey, secrets.OpenRouterAPIKey); err != nil {
		t.Fatal(err)
	}
	if err := e.secrets.Set(context.Background(), config.KeyOpenRouterBaseURL, secrets.OpenRouterBaseURL); err != nil {
		t.Fatal(err)
	}
	e.roles = airolewire.Resolve(context.Background(), db, e.settings, secrets).WithSecretStore(e.secrets)
	e.svc = e.roles.Settings(e.settings, e.secrets)
	e.live = liveJev(e.roles)
	e.build(secrets)
	return e
}

// build makes Sol (with its own SOL_* override), Opus and Luna (none) as services_build.go does.
func (e *hotEnv) build(secrets config.Secrets) {
	e.sol = assist.NewSol(nil, e.roles.Sol(assist.Config{Label: "sol", BaseURL: secrets.SolBaseURL, APIKey: secrets.SolAPIKey}, e.live))
	e.opus = assist.NewOpus(nil, e.roles.Opus(assist.Config{Label: "opus"}, e.live))
	e.luna = assist.NewLuna(nil, e.roles.Luna(assist.Config{Label: "luna"}, e.live))
}

// callAll calls Sol, Opus and Luna once. Every mock answers 400, so each call fails, but
// only after its request has been seen (or, for a skipped role, with ErrNotConfigured).
func (e *hotEnv) callAll() (sol, opus, luna error) {
	ctx := context.Background()
	_, _, sol = e.sol.Analyze(ctx, solInput())
	_, _, opus = e.opus.Review(ctx, reviewInput())
	_, luna = e.luna.Classify(ctx, assist.NewsItem{Symbol: "7203", Headline: "h", PublishedAt: time.Now()})
	return sol, opus, luna
}

func (e *hotEnv) save(t *testing.T, role, provider, model string) {
	t.Helper()
	if err := e.svc.Save(context.Background(), role, provider, model); err != nil {
		t.Fatal(err)
	}
}

func solInput() assist.SolAnalysisInput {
	return assist.SolAnalysisInput{Long: assist.DirectionCalibration{
		Thresholds: config.PolicyDirectionThresholds{MinProbability: 0.60, MinEntryQuality: domain.JevEntryQualityGood},
		Calibration: domain.CalibrationMetrics{
			Buckets:     []domain.ConfidenceBucket{{Range: "0.60-0.70", DirectionAccuracy: 0.4, AvgFutureReturnPct: -0.5, SampleCount: 40}},
			SampleCount: 40,
		},
	}}
}

func reviewInput() assist.OpusReviewInput {
	return assist.OpusReviewInput{
		RationaleJSON: `{"why":"x"}`,
		Changes:       []domain.PolicyChange{{Key: domain.PolicyKeyLongMinProbability, OldValue: "0.6", NewValue: "0.65"}},
		Comparison: assist.BacktestComparison{BaselineExpectancy: 0.2, CandidateExpectancy: 0.25, BaselineMaxDrawdownPct: 5,
			CandidateMaxDrawdownPct: 5, BaselineTradeCount: 300, CandidateTradeCount: 300, BacktestDays: 30},
	}
}

// Nothing saved: Sol uses its own API, Opus and Luna the live Jev, exactly as before the
// roles were read on every call.
func TestAIRoles_HotReload_DefaultUnchangedWhenNothingSaved(t *testing.T) {
	e := newHotEnv(t)
	sol, opus, luna := e.callAll()
	for role, err := range map[string]error{"sol": sol, "opus": opus, "luna": luna} {
		if err == nil || errors.Is(err, assist.ErrNotConfigured) {
			t.Errorf("%s: err = %v, want the mock's 400 (the role must be called)", role, err)
		}
	}
	e.srv.sol.Expect(t, "Sol's own API", hit{assist.SolAnalyzePath, "Bearer fake-sol-key", ""})
	e.srv.jev.Expect(t, "Opus and Luna via the live Jev",
		hit{"/v1/systemone", "Bearer fake-jev-key", "jev-custom"}, hit{"/v1/systemone", "Bearer fake-jev-key", "jev-custom"})
	e.srv.or.Expect(t, "OpenRouter is not called")
}

// A save through the Settings service applies to the very next call of Sol, Opus and Luna
// (provider, model, and the OpenRouter key saved later), and a reset puts the default back.
func TestAIRoles_HotReload_SolOpusLunaSwitchAfterSave(t *testing.T) {
	e := newHotEnv(t)
	for _, role := range []string{airole.RoleSol, airole.RoleOpus, airole.RoleLuna} {
		e.save(t, role, airole.ProviderOpenRouter, "vendor/"+role)
	}
	_, _, _ = e.callAll()
	e.srv.or.Expect(t, "after the save",
		hit{"/v1/systemone", "Bearer fake-or-key", "vendor/sol"},
		hit{"/v1/systemone", "Bearer fake-or-key", "vendor/opus"},
		hit{"/v1/systemone", "Bearer fake-or-key", "vendor/luna"})
	e.srv.sol.Expect(t, "Sol's own API is dropped on OpenRouter")
	e.srv.jev.Expect(t, "the live Jev connection must not serve an OpenRouter role")

	// A model change and a new OpenRouter key apply at once as well.
	e.save(t, airole.RoleSol, airole.ProviderOpenRouter, "vendor/sol-2")
	if err := e.secrets.Set(context.Background(), config.KeyOpenRouterAPIKey, "fake-or-key-2"); err != nil {
		t.Fatal(err)
	}
	_, _, _ = e.callAll()
	e.srv.or.Expect(t, "after a new model and key",
		hit{"/v1/systemone", "Bearer fake-or-key-2", "vendor/sol-2"},
		hit{"/v1/systemone", "Bearer fake-or-key-2", "vendor/opus"},
		hit{"/v1/systemone", "Bearer fake-or-key-2", "vendor/luna"})

	for _, role := range []string{airole.RoleSol, airole.RoleOpus, airole.RoleLuna} {
		if err := e.svc.Reset(context.Background(), role); err != nil {
			t.Fatal(err)
		}
	}
	_, _, _ = e.callAll()
	e.srv.sol.Expect(t, "reset: Sol's own API again", hit{assist.SolAnalyzePath, "Bearer fake-sol-key", ""})
	e.srv.jev.Expect(t, "reset: Opus and Luna via the live Jev again",
		hit{"/v1/systemone", "Bearer fake-jev-key", "jev-custom"}, hit{"/v1/systemone", "Bearer fake-jev-key", "jev-custom"})
	e.srv.or.Expect(t, "reset: no OpenRouter call")
}

// The live Jev is not hot reloaded: a saved Jev role changes nothing for the client built
// at start-up (nor does a Sol/Opus/Luna save), only a restart (a new Resolve) does.
func TestAIRoles_HotReload_LiveJevUnchangedUntilRestart(t *testing.T) {
	e := newHotEnv(t)
	e.save(t, airole.RoleJev, airole.ProviderOpenRouter, "vendor/jev")
	e.save(t, airole.RoleSol, airole.ProviderOpenRouter, "vendor/sol")
	ask(t, e.live)
	e.srv.jev.Expect(t, "live Jev after the saves", hit{"/v1/systemone", "Bearer fake-jev-key", "jev-custom"})
	e.srv.or.Expect(t, "live Jev must not move to OpenRouter")
	if c := e.roles.Connection(airole.RoleJev); c.Provider != airole.ProviderCustom || c.Model != "jev-custom" {
		t.Errorf("start-up connection of the live Jev = %+v, want unchanged", c)
	}

	// Sol, Opus and Luna do not depend on the Jev role's setting.
	_, _, _ = e.callAll()
	e.srv.or.Expect(t, "Sol only", hit{"/v1/systemone", "Bearer fake-or-key", "vendor/sol"})
	e.srv.jev.Expect(t, "Opus and Luna still on the live Jev's custom connection",
		hit{"/v1/systemone", "Bearer fake-jev-key", "jev-custom"}, hit{"/v1/systemone", "Bearer fake-jev-key", "jev-custom"})
	e.srv.sol.Expect(t, "Sol's override is dropped on OpenRouter")

	// A restart resolves the Jev role anew.
	_, secrets := newServersAt(e.srv)
	restarted := airolewire.Resolve(context.Background(), nil, e.settings, secrets)
	ask(t, liveJev(restarted))
	e.srv.or.Expect(t, "live Jev after a restart", hit{"/v1/systemone", "Bearer fake-or-key", "vendor/jev"})
}

// A stored value that cannot be used (invalid, unreadable, OpenRouter without its key, an
// unreadable key store) skips the role: nothing is called, least of all another provider.
func TestAIRoles_HotReload_BrokenStoredValueSkipsRole(t *testing.T) {
	e := newHotEnv(t)
	skipped := func(what string) {
		t.Helper()
		sol, opus, luna := e.callAll()
		for role, err := range map[string]error{"sol": sol, "opus": opus, "luna": luna} {
			if !errors.Is(err, assist.ErrNotConfigured) {
				t.Errorf("%s (%s): err = %v, want ErrNotConfigured (skipped)", role, what, err)
			}
		}
		if e.luna.Configured() {
			t.Errorf("luna (%s) is Configured", what)
		}
		e.srv.sol.Expect(t, what+": Sol's API")
		e.srv.jev.Expect(t, what+": the live Jev")
		e.srv.or.Expect(t, what+": OpenRouter")
	}

	for _, role := range []string{airole.RoleSol, airole.RoleOpus, airole.RoleLuna} {
		if err := e.settings.Set(context.Background(), airole.Key(role), `{"provider":"openrouter"}`, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	skipped("invalid stored value")

	// Repaired from the screen: the roles run again.
	for _, role := range []string{airole.RoleSol, airole.RoleOpus, airole.RoleLuna} {
		e.save(t, role, airole.ProviderOpenRouter, "vendor/"+role)
	}
	_, _, _ = e.callAll()
	e.srv.or.Expect(t, "repaired", hit{"/v1/systemone", "Bearer fake-or-key", "vendor/sol"},
		hit{"/v1/systemone", "Bearer fake-or-key", "vendor/opus"}, hit{"/v1/systemone", "Bearer fake-or-key", "vendor/luna"})

	// OpenRouter without its key: skipped, not moved to the 独自 API key.
	if err := e.secrets.Delete(context.Background(), config.KeyOpenRouterAPIKey); err != nil {
		t.Fatal(err)
	}
	skipped("OpenRouter without a key")

	// An unreadable settings store: skipped, not read as "nothing saved".
	e.roles = airolewire.Resolve(context.Background(), nil, brokenStore{}, config.Secrets{JevAPIKey: "fake-jev-key", JevBaseURL: e.srv.jev.URL})
	e.build(config.Secrets{})
	skipped("unreadable settings")

	// An unreadable key store while on OpenRouter: skipped, the start-up key is not used.
	_, secrets := newServersAt(e.srv)
	e.roles = airolewire.Resolve(context.Background(), nil, e.settings, secrets).WithSecretStore(brokenKeys{})
	e.build(config.Secrets{})
	skipped("unreadable key store")
}

type brokenStore struct{}

func (brokenStore) Get(context.Context, string) (string, bool, error) {
	return "", false, errors.New("store down")
}

type brokenKeys struct{}

func (brokenKeys) Get(context.Context, string) (string, bool, error) {
	return "", false, errors.New("secrets down")
}
