package assist

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

// Asker is the Jev (TypeSafe /v1/systemone) backend Luna, Sol and Opus use
// when no LUNA_*/SOL_*/OPUS_* override is configured (issue #273).
// *jev.Client implements it; an interface here keeps this package free of
// the jev package itself.
//
// Jev answers typed questions only (noul / choice) and returns no free
// text, so each role maps onto structured questions: Luna classifies with
// choice questions, Sol asks Jev to pick among candidates the code
// generated, and Opus asks a noul adoption probability.
type Asker interface {
	// Configured reports whether Jev can be called at all (JEV_API_KEY set).
	Configured() bool
	// Ask evaluates questions about state; label names the call in logs.
	Ask(ctx context.Context, label string, state any, questions map[string]systemone.Question) (systemone.Result, error)
}

// useJev picks a role's backend. A configured override client wins; else Jev
// is used when configured; else the role is disabled (ErrNotConfigured,
// which every caller treats as "skip", never as a startup failure).
func useJev(client *Client, jev Asker) (bool, error) {
	switch {
	case client.Configured():
		return false, nil
	case jev != nil && jev.Configured():
		return true, nil
	}
	return false, ErrNotConfigured
}

// Option configures Luna, Sol or Opus.
type Option func(*options)

type options struct {
	jev Asker
}

// WithJev makes the role fall back to Jev (issue #273) whenever its own
// override client is not configured.
func WithJev(jev Asker) Option {
	return func(o *options) { o.jev = jev }
}

func applyOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
