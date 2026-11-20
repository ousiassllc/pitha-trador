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

// Backend resolves, for one call, a role's override client and its Jev-protocol
// fallback (see useJev). Luna, Sol and Opus ask it at call time, so a provider
// or model saved on the Settings screen applies to the next call without a
// restart. An error means the role cannot be served right now: the call is
// skipped (the error wraps ErrNotConfigured) instead of moving to a backend
// the operator did not choose.
type Backend func(ctx context.Context) (*Client, Asker, error)

// Option configures Luna, Sol or Opus.
type Option func(*options)

type options struct {
	jev     Asker
	backend Backend
}

// WithJev makes the role fall back to Jev (issue #273) whenever its own
// override client is not configured.
func WithJev(jev Asker) Option {
	return func(o *options) { o.jev = jev }
}

// WithBackend makes the role resolve its client and fallback on every call
// instead of using the ones given at construction (client and WithJev are then
// not used; client may be nil).
func WithBackend(b Backend) Option {
	return func(o *options) { o.backend = b }
}

// backendFor is the role's Backend: WithBackend's, else the fixed client and
// WithJev's fallback.
func backendFor(client *Client, opts []Option) Backend {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.backend != nil {
		return o.backend
	}
	return func(context.Context) (*Client, Asker, error) { return client, o.jev, nil }
}

// pick resolves b for one call and says how to serve it (see useJev).
func pick(ctx context.Context, b Backend) (client *Client, jev Asker, viaJev bool, err error) {
	client, jev, err = b(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	viaJev, err = useJev(client, jev)
	return client, jev, viaJev, err
}
