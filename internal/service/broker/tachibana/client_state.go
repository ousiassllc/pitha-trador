package tachibana

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

// SessionObserver is told about session-level outcomes of requests; the
// session manager (tachibana/session) implements it.
type SessionObserver interface {
	// SessionLost: the broker said the session behind generation gen is gone
	// (p_errno=2).
	SessionLost(gen uint64)
	// ClockSkew: the broker rejected p_sd_date (p_errno=8).
	ClockSkew()
	// RequestSucceeded: a request got a clean answer.
	RequestSucceeded()
}

// SetObserver registers the observer of session-level outcomes.
func (c *Client) SetObserver(o SessionObserver) {
	c.mu.Lock()
	c.observer = o
	c.mu.Unlock()
}

// SetOutOfHours records that the broker is closed (03:30〜05:30) or open
// again: while closed, "no session" is expected and is not a feed failure.
func (c *Client) SetOutOfHours(closed bool) { c.outOfHours.Store(closed) }

// afterSession records the outcome of a request made on session generation
// gen and reports session-level failures to the observer.
func (c *Client) afterSession(ctx context.Context, gen uint64, err error) {
	c.record(ctx, err)
	c.mu.Lock()
	obs := c.observer
	c.mu.Unlock()
	var apiErr *APIError
	switch {
	case err == nil:
		if obs != nil {
			obs.RequestSucceeded()
		}
	case errors.As(err, &apiErr) && apiErr.Kind() == KindSessionExpired:
		c.dropSession(gen)
		if obs != nil {
			obs.SessionLost(gen)
		}
	case errors.As(err, &apiErr) && apiErr.Kind() == KindClock:
		if obs != nil {
			obs.ClockSkew()
		}
	}
}

// record feeds one outcome into the market_data_down and broker_api_error
// streaks. A canceled caller is not an outcome.
func (c *Client) record(ctx context.Context, err error) {
	if ctx.Err() != nil {
		return
	}
	switch {
	case err == nil:
		c.boardFailures.Succeed()
		c.brokerFailures.Succeed()
	case errors.Is(err, broker.ErrNoSession) && c.outOfHours.Load():
		// Closed for the night: expected, not a feed failure.
	default:
		if countsAsFeedFailure(err) {
			c.boardFailures.Fail()
		}
		if countsAsBrokerAPIError(err) {
			c.brokerFailures.Fail()
		}
	}
}

func (c *Client) noSessionError() error {
	if c.outOfHours.Load() {
		return fmt.Errorf("%w: the broker is closed (03:30〜05:30), the session resumes after it opens", broker.ErrNoSession)
	}
	return broker.ErrNoSession
}

// session returns the current virtual URLs; ok is false when there is none or
// the 03:30 close has passed.
func (c *Client) session() (virtualURLs, uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.haveURLs {
		return virtualURLs{}, 0, false
	}
	if !c.validUntil.IsZero() && !c.clock.Now().Before(c.validUntil) {
		c.outOfHours.Store(true)
		return virtualURLs{}, 0, false
	}
	return c.urls, c.gen, true
}

// setSession installs the virtual URLs of a fresh login, valid until the
// 03:30 close.
func (c *Client) setSession(urls virtualURLs, validUntil time.Time) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.urls, c.haveURLs, c.validUntil = urls, true, validUntil
	c.gen++
	c.outOfHours.Store(false)
	close(c.changed)
	c.changed = make(chan struct{})
	return c.gen
}

// dropSession forgets the session of generation gen (a no-op for a newer one).
func (c *Client) dropSession(gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen == gen {
		c.urls, c.haveURLs, c.validUntil = virtualURLs{}, false, time.Time{}
	}
}

// BoardFailures is the market_data_down streak: feed-level failures only
// (HTTP/transport errors, p_errno -2/-3/9/-12, a dead session). Closed hours
// (-62) and per-symbol "no data" are not counted.
func (c *Client) BoardFailures() *domain.FailureStreak { return &c.boardFailures }

// BrokerFailures is the broker_api_error streak: consecutive HTTP 5xx.
func (c *Client) BrokerFailures() *domain.FailureStreak { return &c.brokerFailures }
