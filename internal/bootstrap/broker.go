package bootstrap

import (
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu/pushfeed"
)

// newBroker is the one place a broker adapter is chosen (integrations.md §5.1);
// everything downstream takes the neutral broker.Broker. It always returns the
// kabu adapter for now (the selection setting is #723, a second adapter #724).
// kabuBaseURL is empty in production (marketdata.DefaultBaseURL); universe is
// what the adapter subscribes to without a ranking watch list. Nothing is
// started here (broker.Session.Start does that).
func newBroker(secrets config.Secrets, kabuBaseURL string, kabuInfoAPIMaxPerSecond int, universe pushfeed.Universe) broker.Broker {
	client := marketdata.NewClient(marketdata.Config{
		BaseURL:             kabuBaseURL,
		APIPassword:         secrets.KabuAPIPassword,
		InfoAPIMaxPerSecond: kabuInfoAPIMaxPerSecond,
	})
	return kabu.New(client, universe)
}
