package bootstrap

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
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
// runtime_settings choice (LoadBrokerSettings). Only the kabu adapter exists
// so far: until the 立花 adapter lands (issue #724) a tachibana selection is
// reported as an error in the log and kabu is started, so a saved selection
// can never keep the app from coming up. kabuBaseURL is empty in production
// (marketdata.DefaultBaseURL); universe is what the adapter subscribes to
// without a ranking watch list. Nothing is started here (broker.Session.Start
// does that).
func newBroker(selected config.BrokerSettings, secrets config.Secrets, kabuBaseURL string, kabuInfoAPIMaxPerSecond int, universe pushfeed.Universe) broker.Broker {
	if selected.Provider == config.BrokerTachibana {
		slog.Error("bootstrap: broker.provider=tachibana is selected but the tachibana adapter is not implemented yet (issue #724); starting with kabu",
			"environment", selected.Tachibana.Environment)
	}
	client := marketdata.NewClient(marketdata.Config{
		BaseURL:             kabuBaseURL,
		APIPassword:         secrets.KabuAPIPassword,
		InfoAPIMaxPerSecond: kabuInfoAPIMaxPerSecond,
	})
	return kabu.New(client, universe)
}
