// Package jevprompt is the application side of the Jev prompt versioning and
// shadow comparison (issue #872) that the Settings screen uses: it reads the
// state (live version, candidate, switch, daily cost cap, cost today and in
// total, recent promotions), saves the shadow switch, candidate and cap, and
// performs the explicit promotion. It is the GUI twin of the MCP's
// jev_shadow_cost / jev_prompt_promote (internal/service/paperresultsmcp): both
// store the same runtime_settings keys (jevshadowsettings) and the same
// jev_prompt_promotions audit row (Promote).
//
// Nothing here runs by itself: every change is one operator action, and
// nothing in the self-improvement loop or the trading flow calls this package.
package jevprompt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevshadow"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// ErrInvalidInput is wrapped by every rejected operator input; the message is
// an operator-facing reason.
var ErrInvalidInput = errors.New("jevprompt: invalid input")

// DailyHistoryDays is how many JST days of shadow cost State lists.
const DailyHistoryDays = 14

// RecentPromotionCount is how many promotions State lists.
const RecentPromotionCount = 10

// State is everything the Settings panel shows.
type State struct {
	// ShadowRuns is false outside Paper: the shadow never runs in Live.
	ShadowRuns       bool
	Enabled          bool
	LiveVersion      string
	CandidateVersion string // "" for none
	Versions         []string
	CapUSD           float64
	// CapIsDefault: the cap is the proposal (暫定の案・未承認), not a value a
	// person stored.
	CapIsDefault bool
	// FailuresToday and FailureCap: the failed shadow calls of today (JST) and
	// the cap on them (jevshadowsettings.ProposedDailyFailureCap, 暫定の案・未承認).
	FailuresToday int
	FailureCap    int
	Today         judgement.DayCost
	Total         judgement.Spend
	Daily         []judgement.DayCost
	// HaltToday is set when the cost or failure cap stopped the shadow today (JST).
	HaltToday  *jevshadowsettings.Halt
	Promotions []jevshadow.Promotion
}

// Service reads and changes the shadow and promotion state.
type Service struct {
	db       *sql.DB
	settings *system.RuntimeSettingsRepository
	results  *judgement.ShadowRepository
	now      func() time.Time
}

// New returns a Service over the application database.
func New(db *sql.DB) *Service {
	return &Service{db: db, settings: system.NewRuntimeSettingsRepository(db), results: judgement.NewShadowRepository(db), now: time.Now}
}

// SetClock replaces the clock; tests use it.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// State reads the current state.
func (s *Service) State(ctx context.Context) (State, error) {
	cfg := jevshadowsettings.Load(ctx, s.settings)
	now := s.now()
	st := State{
		ShadowRuns:       metalabelsettings.RuntimeMode == metalabelsettings.ModePaper,
		Enabled:          cfg.Enabled,
		LiveVersion:      jev.LiveVersion(ctx, s.settings),
		CandidateVersion: cfg.CandidateVersion,
		Versions:         jev.TraderVersions(),
		CapUSD:           cfg.CapUSD,
		CapIsDefault:     cfg.CapIsDefault,
		FailureCap:       jevshadowsettings.ProposedDailyFailureCap,
	}
	from, to := jevshadowsettings.DayBounds(now)
	spend, err := s.results.SpentBetween(ctx, from, to)
	if err != nil {
		return State{}, err
	}
	st.Today = judgement.DayCost{Date: jevshadowsettings.Day(now), Spend: spend}
	st.FailuresToday = cfg.Failures.FailuresOn(st.Today.Date)
	if st.Total, err = s.results.TotalSpent(ctx); err != nil {
		return State{}, err
	}
	if st.Daily, err = s.results.DailyCosts(ctx, DailyHistoryDays); err != nil {
		return State{}, err
	}
	if h := cfg.Halt; h != nil && h.Date == st.Today.Date {
		st.HaltToday = h
	}
	if st.Promotions, err = judgement.ListPromotions(ctx, s.db, RecentPromotionCount); err != nil {
		return State{}, err
	}
	return st, nil
}

// SetEnabled turns the shadow on or off.
func (s *Service) SetEnabled(ctx context.Context, on bool) error {
	return s.settings.Set(ctx, jevshadowsettings.KeyEnabled, jevshadowsettings.Switch(on), s.now())
}

// SetCandidate chooses the candidate version the shadow runs; "" clears it.
// The version must be registered in this build and differ from the live one
// (a shadow of the live prompt compares nothing).
func (s *Service) SetCandidate(ctx context.Context, version string) error {
	if version == "" {
		return s.settings.Delete(ctx, jevshadowsettings.KeyCandidateVersion)
	}
	if !jev.IsTraderVersion(version) {
		return fmt.Errorf("%w: 版 %q はこのビルドに登録されていません", ErrInvalidInput, version)
	}
	if live := jev.LiveVersion(ctx, s.settings); version == live {
		return fmt.Errorf("%w: 候補の版がライブの版（%s）と同じです。比較にならないため選べません", ErrInvalidInput, live)
	}
	value, err := jevshadowsettings.EncodeVersion(version)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	return s.settings.Set(ctx, jevshadowsettings.KeyCandidateVersion, value, s.now())
}

// SetCap saves the daily cost cap in USD.
func (s *Service) SetCap(ctx context.Context, usd float64) error {
	if err := jevshadowsettings.ValidateCap(usd); err != nil {
		return fmt.Errorf("%w: 1 日の費用上限は 0 より大きく %g USD 以下で指定してください", ErrInvalidInput, jevshadowsettings.MaxDailyCostCapUSD)
	}
	return s.settings.Set(ctx, jevshadowsettings.KeyDailyCostCapUSD, jsonNumber(usd), s.now())
}

// ResetCap removes the stored cap: the proposed default
// (jevshadowsettings.ProposedDailyCostCapUSD, 暫定の案・未承認) applies again.
func (s *Service) ResetCap(ctx context.Context) error {
	return s.settings.Delete(ctx, jevshadowsettings.KeyDailyCostCapUSD)
}

// Promote switches the live Trader prompt version to version and records who
// did it (the GUI), when, and the previous and new versions, in one transaction
// (Promote). It is only ever called by the Settings screen's explicit
// promote button.
func (s *Service) Promote(ctx context.Context, version string) (jevshadow.Promotion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return jevshadow.Promotion{}, fmt.Errorf("jevprompt: begin promotion: %w", err)
	}
	p, err := Promote(ctx, tx, s.now().UTC(), jevshadow.ActorGUI, "", version)
	if err != nil {
		_ = tx.Rollback()
		switch {
		case errors.Is(err, ErrUnknownVersion):
			return jevshadow.Promotion{}, fmt.Errorf("%w: 版 %q はこのビルドに登録されていません", ErrInvalidInput, version)
		case errors.Is(err, ErrAlreadyLive):
			return jevshadow.Promotion{}, fmt.Errorf("%w: 版 %q はすでにライブの版です", ErrInvalidInput, version)
		}
		return jevshadow.Promotion{}, err
	}
	if err := tx.Commit(); err != nil {
		return jevshadow.Promotion{}, fmt.Errorf("jevprompt: commit promotion: %w", err)
	}
	return p, nil
}

func jsonNumber(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
