package gmo

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Defaults applied to zero Config fields.
const (
	// DefaultBaseURL is GMO's public FX API (no authentication).
	DefaultBaseURL = "https://forex-api.coin.z.com/public"
	// MinCallInterval is the floor of Config.MinInterval: at most one call
	// per second (issue #907).
	MinCallInterval       = time.Second
	DefaultMaxAttempts    = 5
	DefaultInitialBackoff = time.Second
	DefaultMaxBackoff     = 30 * time.Second
	defaultHTTPTimeout    = 10 * time.Second
)

// symbolPattern is GMO's pair naming, e.g. USD_JPY, EUR_GBP.
var symbolPattern = regexp.MustCompile(`^[A-Z]{3}_[A-Z]{3}$`)

// Config is the adapter's own settings; it is deliberately independent of
// the application's config files (no keys or defaults there change).
type Config struct {
	// BaseURL defaults to DefaultBaseURL; tests point it at an httptest server.
	BaseURL string
	// Symbols are the pairs the Client serves, e.g. ["USD_JPY"]. Required.
	Symbols []string
	// MinInterval is the minimum spacing between HTTP calls (retries
	// included). Zero means MinCallInterval; smaller values are rejected.
	MinInterval time.Duration
	// MaxAttempts is the number of tries per request (first call included)
	// before giving up. Zero means DefaultMaxAttempts.
	MaxAttempts int
	// InitialBackoff is the wait before the first retry, doubled after each
	// further failure up to MaxBackoff. Zero values mean the defaults.
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// withDefaults returns c with zero fields filled in.
func (c Config) withDefaults() Config {
	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	if c.MinInterval == 0 {
		c.MinInterval = MinCallInterval
	}
	if c.MaxAttempts == 0 {
		c.MaxAttempts = DefaultMaxAttempts
	}
	if c.InitialBackoff == 0 {
		c.InitialBackoff = DefaultInitialBackoff
	}
	if c.MaxBackoff == 0 {
		c.MaxBackoff = DefaultMaxBackoff
	}
	return c
}

// validate checks a defaulted Config.
func (c Config) validate() error {
	if len(c.Symbols) == 0 {
		return errors.New("gmo: Config.Symbols is empty")
	}
	seen := make(map[string]bool, len(c.Symbols))
	for _, s := range c.Symbols {
		if !symbolPattern.MatchString(s) {
			return fmt.Errorf("gmo: invalid symbol %q (want e.g. USD_JPY)", s)
		}
		if seen[s] {
			return fmt.Errorf("gmo: duplicate symbol %q in Config.Symbols", s)
		}
		seen[s] = true
	}
	if c.MinInterval < MinCallInterval {
		return fmt.Errorf("gmo: Config.MinInterval %s is below the %s minimum", c.MinInterval, MinCallInterval)
	}
	if c.MaxAttempts < 1 {
		return fmt.Errorf("gmo: Config.MaxAttempts %d must be >= 1", c.MaxAttempts)
	}
	if c.InitialBackoff < 0 || c.MaxBackoff < c.InitialBackoff {
		return fmt.Errorf("gmo: backoff must satisfy 0 <= InitialBackoff (%s) <= MaxBackoff (%s)", c.InitialBackoff, c.MaxBackoff)
	}
	return nil
}
