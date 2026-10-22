// Package dailyhalt reads today's daily entry halt (FR-RISK-8) for
// health_today. It is split out of query to keep that directory within its
// size limit, and takes the runtime_settings reader as an interface so it does
// not import query (which imports it).
package dailyhalt

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Key is the runtime_settings key of the risk engine's daily guard row.
const Key = "system.daily_guard"

// Store is the read-only runtime_settings view; query's settingsReader
// implements it and refuses any key outside its readable list.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// State is today's daily entry halt (FR-RISK-8).
type State struct {
	Halted bool `json:"halted"`
	// Reason is the halt's fixed code (daily_loss_limit_jpy, profit_lock,
	// max_daily_trades); the message text is never returned.
	Reason   *string `json:"reason"`
	HaltedAt *string `json:"halted_at"`
}

// row is the part of the risk engine's system.daily_guard row health_today
// uses; the halt's message text is deliberately not decoded.
type row struct {
	Day        string `json:"day"`
	HaltReason string `json:"halt_reason"`
	HaltedAt   string `json:"halted_at"`
}

// Today reports the daily entry halt of day the way the app does
// (risk.Engine.DailyHalt): the latched row of that trading day, and only while
// the rule behind it is still enabled in the thresholds.
func Today(ctx context.Context, store Store, day string) (State, error) {
	raw, ok, err := store.Get(ctx, Key)
	if err != nil || !ok {
		return State{}, err
	}
	var r row
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return State{}, fmt.Errorf("query: decode daily guard state: %w", err)
	}
	if r.Day != day || r.HaltReason == "" {
		return State{}, nil
	}
	limits, err := dailyguardsettings.Load(ctx, store)
	if err != nil {
		return State{}, err
	}
	if !ruleEnabled(limits, r.HaltReason) {
		return State{}, nil
	}
	g := State{Halted: true, Reason: &r.HaltReason}
	if at, err := time.Parse(time.RFC3339Nano, r.HaltedAt); err == nil {
		s := at.UTC().Format(time.RFC3339)
		g.HaltedAt = &s
	}
	return g, nil
}

// ruleEnabled is risk's reasonEnabled: a halt of a rule since disabled in
// Settings no longer applies.
func ruleEnabled(l dailyguardsettings.Limits, reason string) bool {
	switch reason {
	case domain.DailyHaltReasonLossLimit:
		return l.LossLimitEnabled()
	case domain.DailyHaltReasonProfitLock:
		return l.ProfitLockEnabled()
	case domain.DailyHaltReasonMaxTrades:
		return l.MaxTradesEnabled()
	}
	return false
}
