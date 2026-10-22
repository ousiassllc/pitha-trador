package healthalert

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/safego"
)

// MarketDataQueue is the job queue whose silence market_data_idle watches.
const MarketDataQueue = "market-data"

// TokenState is what the Detector needs of the broker session: when the
// current not-logged-in streak (kabu 4001007) began, and when the current
// connection-refused streak (kabuステーション not running; issue #863) began.
// Each is the zero time while the session is fine or fails for another reason.
type TokenState struct {
	NotLoggedInSince time.Time
	UnreachableSince time.Time
}

// Store persists the active alerts and the Detector's heartbeat.
type Store interface {
	// Sync makes alerts the stored set at now: rows of codes not in alerts are
	// deleted, the others are inserted or updated with their Since (the rules
	// give a stable Since while a condition lasts). It also records now as the
	// last check.
	Sync(ctx context.Context, now time.Time, alerts []domain.HealthAlert) error
	// Active returns the stored alerts.
	Active(ctx context.Context) ([]domain.HealthAlert, error)
}

// Jobs reads the last finished job of a queue.
type Jobs interface {
	// LastFinishedAt is the latest finished_at of a succeeded or failed job of
	// queue; ok is false when there is none.
	LastFinishedAt(ctx context.Context, queue string) (at time.Time, ok bool, err error)
}

// Config wires a Detector. Now defaults to time.Now.
type Config struct {
	Store Store
	Jobs  Jobs
	Token func() TokenState
	Now   func() time.Time
}

// Detector evaluates the alert rules and persists the result.
type Detector struct {
	cfg       Config
	startedAt time.Time
}

// New returns a Detector. The start time counts as market-data activity: a
// restart in mid-session gets the full IdleThreshold before it can alarm.
func New(cfg Config) *Detector {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Detector{cfg: cfg, startedAt: cfg.Now()}
}

// Check evaluates the rules once and stores the alerts that apply: a
// condition that has cleared is deleted from the store.
func (d *Detector) Check(ctx context.Context) error {
	now := d.cfg.Now()
	var alerts []domain.HealthAlert
	token := d.cfg.Token()
	if since, ok := KabuNotLoggedInSince(now, token.NotLoggedInSince); ok {
		alerts = append(alerts, domain.HealthAlert{Code: domain.HealthAlertKabuNotLoggedIn, Since: since})
	}
	if since, ok := KabuUnreachableSince(now, token.UnreachableSince); ok {
		alerts = append(alerts, domain.HealthAlert{Code: domain.HealthAlertKabuUnreachable, Since: since})
	}
	last, _, err := d.cfg.Jobs.LastFinishedAt(ctx, MarketDataQueue)
	if err != nil {
		return fmt.Errorf("healthalert: last market-data job: %w", err)
	}
	if since, ok := IdleSince(now, Later(last, d.startedAt)); ok {
		alerts = append(alerts, domain.HealthAlert{Code: domain.HealthAlertMarketDataIdle, Since: since})
	}
	if err := d.cfg.Store.Sync(ctx, now, alerts); err != nil {
		return fmt.Errorf("healthalert: store alerts: %w", err)
	}
	return nil
}

// Active returns the alerts to show now: the stored ones that are inside the
// hours they watch.
func (d *Detector) Active(ctx context.Context) ([]domain.HealthAlert, error) {
	stored, err := d.cfg.Store.Active(ctx)
	if err != nil {
		return nil, fmt.Errorf("healthalert: read alerts: %w", err)
	}
	now := d.cfg.Now()
	var active []domain.HealthAlert
	for _, a := range stored {
		if InWindow(a.Code, now) {
			active = append(active, a)
		}
	}
	return active, nil
}

// Run checks every CheckInterval, the first time at once, until ctx is done.
func (d *Detector) Run(ctx context.Context) {
	wait := time.Duration(0)
	safego.Loop(ctx, "health alert check", func() time.Duration {
		w := wait
		wait = CheckInterval
		return w
	}, d.Check)
}
