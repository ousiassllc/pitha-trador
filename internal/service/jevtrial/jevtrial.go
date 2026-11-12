// Package jevtrial is the application side of the model-comparison trial that
// the Settings screen uses: it reads the state (switch, arms, the overall daily
// cost cap, today's and the total counted cost, and per arm the cost, failures
// and stop reasons of today) and saves the switch, the arms and the cap.
//
// The trial costs money and runs only in Paper, so the Settings screen is the
// one place that can turn it on: nothing in the MCP (mcpwritesettings) writes
// these keys, and nothing in the self-improvement loop or the trading flow calls
// this package. It stores the runtime_settings keys of jevtrialsettings, which
// the runner (internal/service/jev/trial) reads; it never imports the runner.
package jevtrial

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// ErrInvalidInput is wrapped by every rejected operator input; the message is
// an operator-facing reason.
var ErrInvalidInput = errors.New("jevtrial: invalid input")

// KeyStore is the secrets access State needs to tell whether the OpenRouter key
// is set. Only the "found" flag is ever used; the value is discarded unread.
// *system.SecretsRepository implements it.
type KeyStore interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Spend is a number of trial calls and the cost counted against the daily caps
// (judgement.TrialSpend, re-exported so the web layer does not import the repository).
type Spend = judgement.TrialSpend

// ArmStatus is one configured arm with today's (JST) numbers.
type ArmStatus struct {
	Arm jevtrialsettings.Arm
	// Today is the calls and the cost counted against the caps today; CapUSD is
	// the arm's own daily cap.
	Today  Spend
	CapUSD float64
	// FailuresToday and FailureCap: the arm's failed calls today and the cap.
	FailuresToday int
	FailureCap    int
	// Halt is the arm's stop of today, nil when it is not stopped by a record.
	Halt *jevtrialsettings.Halt
	// Throttle is the arm's last 429/529 stop, nil for none; ThrottleActive is
	// true while the cooldown runs and ThrottlesToday counts today's stops.
	Throttle       *jevtrialsettings.Throttle
	ThrottleActive bool
	ThrottlesToday int
}

// State is everything the Settings panel shows.
type State struct {
	// TrialRuns is false outside Paper: the trial never runs in Live.
	TrialRuns bool
	Enabled   bool
	Arms      []ArmStatus
	// ArmsJSON is the stored arms as the JSON the form edits ("[]" for none).
	ArmsJSON string
	// ArmsError says why the stored arms are ignored ("" when they parse).
	ArmsError string
	// CapUSD is the overall daily cap; CapIsDefault says it is the default, not a
	// value a person stored on the Settings screen.
	CapUSD       float64
	CapIsDefault bool
	TodayDate    string
	// Today and Total are the calls and the counted cost across all arms
	// (unknown costs are counted at the worst case, so these are upper-leaning).
	Today Spend
	Total Spend
	// Halt is the stop of the whole trial today, nil for none.
	Halt *jevtrialsettings.Halt
	// KeyKnown is true when the secrets store could be asked; KeySet then says
	// whether OPENROUTER_API_KEY is set. The value is never read out.
	KeyKnown bool
	KeySet   bool
}

// Service reads and changes the trial settings.
type Service struct {
	settings *system.RuntimeSettingsRepository
	results  *judgement.TrialRepository
	keys     KeyStore
	now      func() time.Time
}

// New returns a Service over the application database.
func New(db *sql.DB) *Service {
	return &Service{settings: system.NewRuntimeSettingsRepository(db), results: judgement.NewTrialRepository(db), now: time.Now}
}

// WithKeyStore lets State report whether OPENROUTER_API_KEY is set.
func (s *Service) WithKeyStore(keys KeyStore) *Service {
	s.keys = keys
	return s
}

// SetClock replaces the clock; tests use it.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// State reads the current state.
func (s *Service) State(ctx context.Context) (State, error) {
	cfg := jevtrialsettings.Load(ctx, s.settings)
	now := s.now()
	st := State{
		TrialRuns:    metalabelsettings.RuntimeMode == metalabelsettings.ModePaper,
		Enabled:      cfg.Enabled,
		ArmsError:    cfg.ArmsError,
		CapUSD:       cfg.CapUSD,
		CapIsDefault: cfg.CapIsDefault,
		TodayDate:    jevshadowsettings.Day(now),
		ArmsJSON:     "[]",
	}
	if len(cfg.Arms) > 0 {
		if b, err := json.MarshalIndent(cfg.Arms, "", "  "); err == nil {
			st.ArmsJSON = string(b)
		}
	}
	from, to := jevshadowsettings.DayBounds(now)
	var err error
	if st.Today, err = s.results.SpentBetween(ctx, from, to); err != nil {
		return State{}, err
	}
	// Every row has a created_at not after now: this interval is "all rows".
	if st.Total, err = s.results.SpentBetween(ctx, time.Unix(0, 0), now.AddDate(1, 0, 0)); err != nil {
		return State{}, err
	}
	if cfg.Halt.HaltOn(st.TodayDate) {
		st.Halt = cfg.Halt
	}
	for _, arm := range cfg.Arms {
		as, err := s.armStatus(ctx, arm, from, to, now, st.TodayDate)
		if err != nil {
			return State{}, err
		}
		st.Arms = append(st.Arms, as)
	}
	if s.keys != nil {
		if _, ok, err := s.keys.Get(ctx, config.KeyOpenRouterAPIKey); err == nil {
			st.KeyKnown, st.KeySet = true, ok
		}
	}
	return st, nil
}

func (s *Service) armStatus(ctx context.Context, arm jevtrialsettings.Arm, from, to, now time.Time, day string) (ArmStatus, error) {
	spend, err := s.results.SpentByArmBetween(ctx, arm.ID, from, to)
	if err != nil {
		return ArmStatus{}, err
	}
	rec := jevtrialsettings.LoadArm(ctx, s.settings, arm.ID)
	as := ArmStatus{
		Arm: arm, Today: spend, CapUSD: arm.CapUSD(),
		FailuresToday: rec.Failures.FailuresOn(day), FailureCap: jevtrialsettings.DailyFailureCap,
		Throttle: rec.Throttle, ThrottleActive: rec.Throttle.ActiveAt(now), ThrottlesToday: rec.Throttle.CountOn(day),
	}
	if rec.Halt.HaltOn(day) {
		as.Halt = rec.Halt
	}
	return as, nil
}

// SetEnabled turns the trial on or off. Turning it off is always allowed.
func (s *Service) SetEnabled(ctx context.Context, on bool) error {
	return s.settings.Set(ctx, jevtrialsettings.KeyEnabled, jevtrialsettings.Switch(on), s.now())
}

// ValidateArms checks the arms JSON the operator typed ("" is no arm): it must
// parse as jevtrialsettings arms and every arm's prompt version must be
// registered in this build. It returns the value to store.
func ValidateArms(armsJSON string) (string, error) {
	armsJSON = strings.TrimSpace(armsJSON)
	if armsJSON == "" {
		armsJSON = "[]"
	}
	arms, err := jevtrialsettings.ParseArms(armsJSON)
	if err != nil {
		return "", fmt.Errorf("%w: アームの設定を読み取れません（%s）", ErrInvalidInput, strings.TrimPrefix(err.Error(), "jevtrialsettings: "))
	}
	for _, a := range arms {
		if !jev.IsTraderVersion(a.PromptVersion) {
			return "", fmt.Errorf("%w: アーム %q のプロンプトの版 %q はこのビルドに登録されていません", ErrInvalidInput, a.ID, a.PromptVersion)
		}
	}
	return jevtrialsettings.EncodeArms(arms)
}

// SetArms saves the arms (a JSON array; "" or "[]" for none).
func (s *Service) SetArms(ctx context.Context, armsJSON string) error {
	value, err := ValidateArms(armsJSON)
	if err != nil {
		return err
	}
	return s.settings.Set(ctx, jevtrialsettings.KeyArms, value, s.now())
}

// ValidateCap checks an overall daily cost cap in USD.
func ValidateCap(usd float64) error {
	if err := jevtrialsettings.ValidateCap(usd); err != nil {
		return fmt.Errorf("%w: 1 日の費用上限は 0 より大きく %g USD 以下で指定してください", ErrInvalidInput, jevshadowsettings.MaxDailyCostCapUSD)
	}
	return nil
}

// SetCap saves the overall daily cost cap in USD.
func (s *Service) SetCap(ctx context.Context, usd float64) error {
	if err := ValidateCap(usd); err != nil {
		return err
	}
	return s.settings.Set(ctx, jevtrialsettings.KeyDailyCostCapUSD, strconv.FormatFloat(usd, 'g', -1, 64), s.now())
}

// ResetCap removes the stored cap: the default
// (jevtrialsettings.DefaultDailyCostCapUSD) applies again.
func (s *Service) ResetCap(ctx context.Context) error {
	return s.settings.Delete(ctx, jevtrialsettings.KeyDailyCostCapUSD)
}

// Form is what the Settings form submits. Cap is nil for a blank field (the
// default applies).
type Form struct {
	Enabled  bool
	ArmsJSON string
	Cap      *float64
}

// Save validates the whole form first and only then writes it, so a rejected
// form changes nothing.
func (s *Service) Save(ctx context.Context, f Form) error {
	if _, err := ValidateArms(f.ArmsJSON); err != nil {
		return err
	}
	if f.Cap != nil {
		if err := ValidateCap(*f.Cap); err != nil {
			return err
		}
	}
	if err := s.SetArms(ctx, f.ArmsJSON); err != nil {
		return err
	}
	var err error
	if f.Cap == nil {
		err = s.ResetCap(ctx)
	} else {
		err = s.SetCap(ctx, *f.Cap)
	}
	if err != nil {
		return err
	}
	return s.SetEnabled(ctx, f.Enabled)
}
