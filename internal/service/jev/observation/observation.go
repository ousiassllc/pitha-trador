// Package observation is the gate behind the observation-only symbols (issue
// #1055): symbols that are watched and asked on the live Jev Scout to collect
// evaluation data faster, but never traded. Gate decides which symbols those
// are (watchsettings: the switch and the symbol list, default off) and bounds
// the extra Jev calls they cause with a daily cost cap and a failed-call cap.
//
// The cap applies to these calls ONLY: the Scout and Trader calls of every
// other symbol (the normal trading path) never consult Gate, so reaching the
// cap cannot stop them. Gate is read by jev.Scout alone; nothing here is read
// by Policy, Risk, order placement or Paper/Live.
package observation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/watchsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/shadow"
)

// Settings is the runtime_settings access Gate needs;
// *system.RuntimeSettingsRepository implements it.
type Settings interface {
	watchsettings.Store
	Set(ctx context.Context, key, value string, updatedAt time.Time) error
}

// Gate implements jev.ObservationGate over runtime_settings.
type Gate struct {
	settings Settings
	now      func() time.Time
	// mu serializes the read-modify-write of the day's usage row.
	mu sync.Mutex
}

var _ jev.ObservationGate = (*Gate)(nil)

// New returns a Gate over settings; now defaults to time.Now.
func New(settings Settings, now func() time.Time) *Gate {
	if now == nil {
		now = time.Now
	}
	return &Gate{settings: settings, now: now}
}

// Observed reports whether symbol is in the effective observation set: the
// switch is on and the symbol is listed. The settings are read on every call,
// so a change applies to the next job.
func (g *Gate) Observed(ctx context.Context, symbol string) (bool, error) {
	set := watchsettings.Load(ctx, g.settings)
	for _, s := range set.Observed() {
		if s == symbol {
			return true, nil
		}
	}
	return false, nil
}

// Admit reports whether one more observation call may be made today (JST): the
// estimated cost so far is below the cap and the day's failed calls are below
// watchsettings.DailyFailureCap.
func (g *Gate) Admit(ctx context.Context) (bool, string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	u, err := g.usage(ctx)
	if err != nil {
		return false, "", err
	}
	capUSD := watchsettings.Load(ctx, g.settings).ObservationCapUSD
	switch {
	case u.CostUSD >= capUSD:
		return false, fmt.Sprintf("daily_cost_cap: spent %.6f of %.6f USD", u.CostUSD, capUSD), nil
	case u.Failures >= watchsettings.DailyFailureCap:
		return false, fmt.Sprintf("daily_failure_cap: %d of %d failed calls", u.Failures, watchsettings.DailyFailureCap), nil
	}
	return true, "", nil
}

// RecordCall adds a completed call to today's usage. Its cost is the one Jev
// reported, or - the API returns usage, not a charge - the input tokens (or
// an estimate from the request's size) at the published price, the same
// estimate the shadow uses (shadow.InputCostUSD), so a call is never counted
// as free.
func (g *Gate) RecordCall(ctx context.Context, resp jev.ScoutResponse) error {
	return g.update(ctx, func(u *watchsettings.Usage) {
		u.Calls++
		u.CostUSD += callCostUSD(resp)
	})
}

// RecordFailure adds a failed call to today's usage.
func (g *Gate) RecordFailure(ctx context.Context) error {
	return g.update(ctx, func(u *watchsettings.Usage) { u.Failures++ })
}

func callCostUSD(resp jev.ScoutResponse) float64 {
	if resp.RequestCost != nil && *resp.RequestCost >= 0 {
		return *resp.RequestCost
	}
	tokens := resp.InputTokens
	if tokens <= 0 {
		tokens = shadow.EstimateInputTokens(resp.RequestBytes)
	}
	return shadow.InputCostUSD(tokens)
}

func (g *Gate) update(ctx context.Context, change func(*watchsettings.Usage)) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	u, err := g.usage(ctx)
	if err != nil {
		return err
	}
	change(&u)
	value, err := watchsettings.EncodeUsage(u)
	if err != nil {
		return err
	}
	if err := g.settings.Set(ctx, watchsettings.KeyObservationUsage, value, g.now()); err != nil {
		return fmt.Errorf("observation: save the day's usage: %w", err)
	}
	return nil
}

// usage reads today's counters; a stored record of another day or one that no
// longer parses counts as zero (a fault can only let more calls through for
// the rest of the day, bounded by the cost the calls themselves record).
func (g *Gate) usage(ctx context.Context) (watchsettings.Usage, error) {
	today := watchsettings.Day(g.now())
	raw, ok, err := g.settings.Get(ctx, watchsettings.KeyObservationUsage)
	if err != nil {
		return watchsettings.Usage{}, fmt.Errorf("observation: read the day's usage: %w", err)
	}
	if !ok {
		return watchsettings.Usage{Date: today}, nil
	}
	u, err := watchsettings.ParseUsage(raw)
	if err != nil {
		return watchsettings.Usage{Date: today}, nil
	}
	return u.On(today), nil
}
