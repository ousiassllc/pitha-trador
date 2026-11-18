package researchhub_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/researchhub"
	"github.com/ousiassllc/pitha-trador/internal/config"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

func newHub(t *testing.T, fb *tt.FakeBroker, provider string, mutate func(*config.BrokerSettings, *config.Secrets)) (*researchhub.Hub, func()) {
	t.Helper()
	broker := config.BrokerSettings{Provider: provider, Tachibana: fb.Settings()}
	secrets := config.Secrets{TachibanaDemoAuthID: tt.AuthID}
	if mutate != nil {
		mutate(&broker, &secrets)
	}
	h := researchhub.New(researchhub.Config{Broker: broker, Secrets: secrets, Base: adapterBase(fb)})
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	h.Start(ctx, &wg)
	return h, func() { cancel(); wg.Wait() }
}

// Nothing is created or sent while the credentials are unset or the broker is
// 立花: the fake broker sees no request at all.
func TestAcquireDoesNotReachTheBrokerWhenUnusable(t *testing.T) {
	for name, tc := range map[string]struct {
		provider string
		mutate   func(*config.BrokerSettings, *config.Secrets)
		want     error
	}{
		"no auth id":              {config.BrokerKabu, func(_ *config.BrokerSettings, s *config.Secrets) { s.TachibanaDemoAuthID = "" }, researchhub.ErrNotConfigured},
		"no private key":          {config.BrokerKabu, func(b *config.BrokerSettings, _ *config.Secrets) { b.Tachibana.DemoPrivateKeyPath = "" }, researchhub.ErrNotConfigured},
		"tachibana is the broker": {config.BrokerTachibana, nil, researchhub.ErrBrokerIsTachibana},
	} {
		t.Run(name, func(t *testing.T) {
			fb := tt.New(t, tt.NewAutoClock(tt.AtJST(2026, 10, 8, 6, 0)))
			h, stop := newHub(t, fb, tc.provider, tc.mutate)
			defer stop()
			if a, err := h.Acquire(context.Background()); !errors.Is(err, tc.want) || a != nil {
				t.Fatalf("Acquire = %v, %v; want %v", a, err, tc.want)
			}
			if _, err := h.StartFeed(context.Background()); !errors.Is(err, tc.want) {
				t.Fatalf("StartFeed error = %v; want %v", err, tc.want)
			}
			if n := len(fb.Requests()); n != 0 {
				t.Fatalf("the broker received %d requests, want 0", n)
			}
		})
	}
}

// With kabu selected and credentials set, one login serves every Acquire.
func TestAcquireLogsInOnceAndSharesTheAdapter(t *testing.T) {
	fb := tt.New(t, tt.NewAutoClock(tt.AtJST(2026, 10, 8, 6, 0)))
	h, stop := newHub(t, fb, config.BrokerKabu, nil)
	defer stop()
	a, err := h.Acquire(context.Background())
	if err != nil || a == nil {
		t.Fatalf("Acquire = %v, %v", a, err)
	}
	b, err := h.Acquire(context.Background())
	if err != nil || b != a {
		t.Fatalf("second Acquire = %v, %v; want the same adapter", b, err)
	}
	if n := fb.Count("CLMAuthLoginRequest"); n != 1 {
		t.Fatalf("logins = %d, want 1", n)
	}
}

func TestAcquireBeforeStartFails(t *testing.T) {
	fb := tt.New(t, tt.NewAutoClock(tt.AtJST(2026, 10, 8, 6, 0)))
	h := researchhub.New(researchhub.Config{
		Broker:  config.BrokerSettings{Provider: config.BrokerKabu, Tachibana: fb.Settings()},
		Secrets: config.Secrets{TachibanaDemoAuthID: tt.AuthID},
	})
	if _, err := h.Acquire(context.Background()); !errors.Is(err, researchhub.ErrNotStarted) {
		t.Fatalf("Acquire before Start = %v, want ErrNotStarted", err)
	}
}
