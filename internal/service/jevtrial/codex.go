package jevtrial

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
)

// CodexStatus is the state of the ChatGPT subscription provider (the Codex CLI's
// login, issue #1081) the Settings screen shows. It holds no credential and no
// text of the CLI's output: the login state, the caps with today's usage and the
// recorded stop (its reason code and time) only.
type CodexStatus struct {
	Enabled bool
	// CallCap and TokenCap are the daily caps; the *IsDefault flags say none was stored.
	CallCap, TokenCap                   int
	CallCapIsDefault, TokenCapIsDefault bool
	// CallsToday and TokensToday are the provider's calls and tokens (input +
	// output) of today (JST).
	CallsToday, TokensToday int
	// Login is the last login check the app made, nil when none was made yet.
	Login *jevtrialsettings.CodexLogin
	// Halt is the stop in force now, nil for none.
	Halt *jevtrialsettings.CodexHalt
}

func (s *Service) codexStatus(ctx context.Context, cfg jevtrialsettings.CodexSettings, from, to, now time.Time) (CodexStatus, error) {
	usage, err := s.results.UsageByProviderBetween(ctx, jevtrialsettings.ProviderCodexChatGPT, from, to)
	if err != nil {
		return CodexStatus{}, err
	}
	st := CodexStatus{
		Enabled: cfg.Enabled, CallCap: cfg.CallCap, TokenCap: cfg.TokenCap,
		CallCapIsDefault: cfg.CallCapIsDefault, TokenCapIsDefault: cfg.TokenCapIsDefault,
		CallsToday: usage.Calls, TokensToday: usage.Tokens, Login: cfg.Login,
	}
	if cfg.Halt.ActiveAt(now) {
		st.Halt = cfg.Halt
	}
	return st, nil
}

// validateCodex checks the Codex part of the form against the arms: an arm of the
// ChatGPT subscription is saved only while the provider's own switch is on in the
// same form, so nothing can start a Codex call before a person turned it on.
func validateCodex(f Form, arms []jevtrialsettings.Arm) error {
	if f.CodexCallCap != nil || f.CodexTokenCap != nil {
		calls, tokens := jevtrialsettings.DefaultCodexDailyCallCap, jevtrialsettings.DefaultCodexDailyTokenCap
		if f.CodexCallCap != nil {
			calls = *f.CodexCallCap
		}
		if f.CodexTokenCap != nil {
			tokens = *f.CodexTokenCap
		}
		if err := jevtrialsettings.ValidateCodexCaps(calls, tokens); err != nil {
			return fmt.Errorf("%w: ChatGPT サブスクの 1 日の上限は、呼び出し回数が 1〜%d・トークン数が 1〜%d の整数で指定してください",
				ErrInvalidInput, jevtrialsettings.MaxCodexDailyCallCap, jevtrialsettings.MaxCodexDailyTokenCap)
		}
	}
	if f.CodexEnabled {
		return nil
	}
	for _, a := range arms {
		if a.Provider == jevtrialsettings.ProviderCodexChatGPT {
			return fmt.Errorf("%w: アーム %s は ChatGPT サブスク（%s）です。「ChatGPT サブスク（Codex）を有効にする」をオンにしてから保存してください",
				ErrInvalidInput, strconv.Quote(a.ID), jevtrialsettings.ProviderCodexChatGPT)
		}
	}
	return nil
}

func (s *Service) setCodexEnabled(ctx context.Context, on bool) error {
	return s.settings.Set(ctx, jevtrialsettings.KeyCodexEnabled, jevtrialsettings.Switch(on), s.now())
}

// setCodexCap stores a cap, or removes the stored one (nil: the default applies).
func (s *Service) setCodexCap(ctx context.Context, key string, v *int) error {
	if v == nil {
		return s.settings.Delete(ctx, key)
	}
	return s.settings.Set(ctx, key, strconv.Itoa(*v), s.now())
}
