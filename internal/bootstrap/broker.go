package bootstrap

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/adapter"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/session"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu/pushfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

// LoadBrokerSettings reads the broker selection and the 立花 settings the
// Settings screen stored in runtime_settings (issue #733); nothing stored
// means kabu, the demo environment and the defaults. A malformed stored row is
// logged and replaced by its default (opsettings.LoadBroker); err is non-nil
// only for a repository/DB failure. The result is read once at start-up: a
// saved change takes effect after a restart.
func LoadBrokerSettings(ctx context.Context, state *State) (config.BrokerSettings, error) {
	return opsettings.LoadBroker(ctx, system.NewRuntimeSettingsRepository(state.DB))
}

// WithBrokerSettings sets the broker selection newBroker honours (default:
// the zero value, i.e. kabu). cmd/desktop and cmd/server pass
// LoadBrokerSettings' result.
func WithBrokerSettings(b config.BrokerSettings) BuildOption {
	return func(s *buildSettings) { s.brokerSettings = b }
}

// newBroker is the one place a broker adapter is chosen (integrations.md §5.1);
// everything downstream takes the neutral broker.Broker. selected is the
// runtime_settings choice (LoadBrokerSettings): kabu (default) or the 立花
// e支店 adapter (issue #724), whose operator notices go to notices. kabuBaseURL
// is empty in production (marketdata.DefaultBaseURL); universe is what the
// kabu adapter subscribes to without a ranking watch list. Nothing is started
// here (broker.Session.Start does that): a 立花 selection with a missing 認証ID
// or 秘密鍵 builds fine and reports the problem as a failed session, the same
// way an unreachable kabuステーション does.
func newBroker(selected config.BrokerSettings, secrets config.Secrets, kabuBaseURL string, kabuInfoAPIMaxPerSecond int, universe pushfeed.Universe, notices session.Notifier) broker.Broker {
	if selected.Provider == config.BrokerTachibana {
		return adapter.New(adapter.Config{
			Settings:    selected.Tachibana,
			Credentials: secrets.TachibanaCredentials(selected.Tachibana.Environment),
			Notifier:    notices,
		})
	}
	client := marketdata.NewClient(marketdata.Config{
		BaseURL:             kabuBaseURL,
		APIPassword:         secrets.KabuAPIPassword,
		InfoAPIMaxPerSecond: kabuInfoAPIMaxPerSecond,
	})
	return kabu.New(client, universe)
}
