package jevtrialsettings

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

// ProviderCodexChatGPT is the arm provider of the ChatGPT subscription through
// the official Codex CLI's login (issue #1081). It is an arm provider only (the
// trial's); no AI role (airole) can name it. An arm of this provider is called
// only while KeyCodexEnabled is on, and its cost is never USD: it is counted by
// calls and tokens (KeyCodexDailyCallCap, KeyCodexDailyTokenCap).
const ProviderCodexChatGPT = "codex_chatgpt"

// Keys of the Codex provider's runtime_settings rows. All of them are provider
// wide (every codex_chatgpt arm shares one ChatGPT account and one login). The
// switch and the two caps are on the MCP's write whitelist (issue #1220); the
// halt and login records are the runner's and are not.
const (
	// KeyCodexEnabled is the Codex provider's own switch: the JSON integer 1 (on)
	// or 0 (off). OFF while nothing is stored or the stored value is damaged. With
	// it off an arm of this provider is never called (and cannot be saved), even
	// when the trial itself is on.
	KeyCodexEnabled = "jev.trial.codex.enabled"
	// KeyCodexDailyCallCap and KeyCodexDailyTokenCap bound the calls and the
	// tokens (input + output) of the provider per JST day (JSON integers).
	KeyCodexDailyCallCap = "jev.trial.codex.daily_call_cap"
	// #nosec G101 -- a runtime_settings key name, not a credential
	KeyCodexDailyTokenCap = "jev.trial.codex.daily_token_cap"
	// KeyCodexHalt records the stop of the whole provider until a time (a JSON
	// CodexHalt).
	KeyCodexHalt = "jev.trial.codex.halt"
	// KeyCodexLogin records the last login check (a JSON CodexLogin): the app
	// writes it, the screen and the MCP (separate processes) read it.
	KeyCodexLogin = "jev.trial.codex.login"
)

// Defaults and bounds of the Codex caps. They are guards, not measured figures:
// the tokens a call uses are not measured yet (the call includes Codex's own
// instructions on top of the state), so the defaults are small.
const (
	DefaultCodexDailyCallCap  = 30
	DefaultCodexDailyTokenCap = 500_000
	MaxCodexDailyCallCap      = 10_000
	MaxCodexDailyTokenCap     = 100_000_000
)

// CodexMinCallInterval is the least time between two calls of one Codex arm:
// every call starts a process, and the account's window is shared with the
// person's own Codex use.
const CodexMinCallInterval = 10 * time.Second

// Reasons of a CodexHalt.
const (
	// CodexHaltUsageLimit: the failed call's text matched a usage-limit message
	// of the Codex CLI. Only a match is called this; any other failure is
	// CodexHaltFailed.
	CodexHaltUsageLimit = "usage_limit"
	// CodexHaltFailed: the call failed (turn.failed or a non-zero exit, a timeout,
	// a process that could not run). A failure stops the provider; it is never
	// retried.
	CodexHaltFailed = "failed"
	// CodexHaltCallCap and CodexHaltTokenCap: today's calls or tokens reached the cap.
	CodexHaltCallCap  = "call_cap"
	CodexHaltTokenCap = "token_cap"
)

// CodexHalt is the record of the provider's stop until Until. It carries no
// text of the CLI's output.
type CodexHalt struct {
	Reason string `json:"reason"`
	// Until is when the provider may be called again (RFC3339). UntilKnown says
	// the CLI gave the time; otherwise it is the end of the JST day of At.
	Until      string `json:"until"`
	UntilKnown bool   `json:"until_known"`
	At         string `json:"at"`
	// Calls/Tokens and their caps are set for the cap reasons.
	Calls  int `json:"calls,omitempty"`
	Tokens int `json:"tokens,omitempty"`
}

// ActiveAt reports whether the stop is still in force at now.
func (h *CodexHalt) ActiveAt(now time.Time) bool {
	if h == nil {
		return false
	}
	until, err := time.Parse(time.RFC3339, h.Until)
	return err == nil && now.Before(until)
}

// EncodeCodexHalt renders h for KeyCodexHalt.
func EncodeCodexHalt(h CodexHalt) (string, error) { return encode(KeyCodexHalt, h) }

// ParseCodexHalt decodes the runtime_settings value of KeyCodexHalt.
func ParseCodexHalt(jsonValue string) (CodexHalt, error) {
	var h CodexHalt
	if err := json.Unmarshal([]byte(jsonValue), &h); err != nil {
		return CodexHalt{}, fmt.Errorf("jevtrialsettings: decode runtime setting %s=%q: %w", KeyCodexHalt, jsonValue, err)
	}
	_, untilErr := time.Parse(time.RFC3339, h.Until)
	_, atErr := time.Parse(time.RFC3339, h.At)
	if untilErr != nil || atErr != nil || h.Reason == "" {
		return CodexHalt{}, fmt.Errorf("jevtrialsettings: runtime setting %s=%q needs a reason and the times", KeyCodexHalt, jsonValue)
	}
	return h, nil
}

// Login states of CodexLogin.State (what `codex login status` showed the app).
const (
	// CodexLoginChatGPT: logged in with a ChatGPT account (the subscription).
	CodexLoginChatGPT = "chatgpt"
	// CodexLoginLoggedOut: not logged in.
	CodexLoginLoggedOut = "logged_out"
	// CodexLoginOtherAuth: logged in, but not shown to be a ChatGPT login (e.g.
	// an API key, which is billed per use) or the output was not recognised; the
	// provider is not used.
	CodexLoginOtherAuth = "other_auth"
	// CodexLoginUnavailable: the check could not be made (no CLI, no dedicated
	// CODEX_HOME, a timeout).
	CodexLoginUnavailable = "unavailable"
)

// CodexLogin is the last login check. It holds the state only, never the CLI's output.
type CodexLogin struct {
	State string `json:"state"`
	At    string `json:"at"`
}

// ValidCodexLoginState reports whether s is one of the CodexLogin* states.
func ValidCodexLoginState(s string) bool {
	switch s {
	case CodexLoginChatGPT, CodexLoginLoggedOut, CodexLoginOtherAuth, CodexLoginUnavailable:
		return true
	}
	return false
}

// EncodeCodexLogin renders l for KeyCodexLogin.
func EncodeCodexLogin(l CodexLogin) (string, error) { return encode(KeyCodexLogin, l) }

// ParseCodexLogin decodes the runtime_settings value of KeyCodexLogin.
func ParseCodexLogin(jsonValue string) (CodexLogin, error) {
	var l CodexLogin
	if err := json.Unmarshal([]byte(jsonValue), &l); err != nil {
		return CodexLogin{}, fmt.Errorf("jevtrialsettings: decode runtime setting %s=%q: %w", KeyCodexLogin, jsonValue, err)
	}
	if _, err := time.Parse(time.RFC3339, l.At); err != nil || !ValidCodexLoginState(l.State) {
		return CodexLogin{}, fmt.Errorf("jevtrialsettings: runtime setting %s=%q needs a known state and a time", KeyCodexLogin, jsonValue)
	}
	return l, nil
}

// ParseCodexCap decodes a stored Codex cap (a JSON integer from 1 to max) of key.
func ParseCodexCap(key, jsonValue string, max int) (int, error) {
	n, err := strconv.Atoi(jsonValue)
	if err != nil || n < 1 || n > max {
		return 0, fmt.Errorf("jevtrialsettings: runtime setting %s=%q must be an integer from 1 to %d", key, jsonValue, max)
	}
	return n, nil
}

// ValidateCodexCaps checks the caps a person typed.
func ValidateCodexCaps(calls, tokens int) error {
	if calls < 1 || calls > MaxCodexDailyCallCap {
		return fmt.Errorf("jevtrialsettings: the daily call cap must be from 1 to %d", MaxCodexDailyCallCap)
	}
	if tokens < 1 || tokens > MaxCodexDailyTokenCap {
		return fmt.Errorf("jevtrialsettings: the daily token cap must be from 1 to %d", MaxCodexDailyTokenCap)
	}
	return nil
}

// CodexSettings is the effective state of the Codex provider's keys.
type CodexSettings struct {
	Enabled bool
	// CallCap and TokenCap are the daily caps; the *IsDefault flags say nothing
	// valid is stored.
	CallCap, TokenCap                   int
	CallCapIsDefault, TokenCapIsDefault bool
	// Halt is the recorded stop (active or not: ask ActiveAt), nil for none.
	Halt *CodexHalt
	// Login is the last login check, nil when none was made yet.
	Login *CodexLogin
}

// loadCodex reads the Codex keys with the same fallbacks as Load: a damaged
// value falls back to off, the default caps, no halt and no login record.
func loadCodex(rd *reader) CodexSettings {
	c := CodexSettings{CallCap: DefaultCodexDailyCallCap, TokenCap: DefaultCodexDailyTokenCap, CallCapIsDefault: true, TokenCapIsDefault: true}
	if raw, ok := rd.get(KeyCodexEnabled); ok {
		if on, err := parseSwitch(KeyCodexEnabled, raw); err == nil {
			c.Enabled = on
		} else {
			slog.Warn("jevtrialsettings: treating a damaged Codex switch as off", "error", err)
		}
	}
	if raw, ok := rd.get(KeyCodexDailyCallCap); ok {
		if n, err := ParseCodexCap(KeyCodexDailyCallCap, raw, MaxCodexDailyCallCap); err == nil {
			c.CallCap, c.CallCapIsDefault = n, false
		} else {
			slog.Warn("jevtrialsettings: using the default Codex daily call cap", "error", err)
		}
	}
	if raw, ok := rd.get(KeyCodexDailyTokenCap); ok {
		if n, err := ParseCodexCap(KeyCodexDailyTokenCap, raw, MaxCodexDailyTokenCap); err == nil {
			c.TokenCap, c.TokenCapIsDefault = n, false
		} else {
			slog.Warn("jevtrialsettings: using the default Codex daily token cap", "error", err)
		}
	}
	if raw, ok := rd.get(KeyCodexHalt); ok {
		if h, err := ParseCodexHalt(raw); err == nil {
			c.Halt = &h
		} else {
			slog.Warn("jevtrialsettings: ignoring the stored Codex halt record", "error", err)
		}
	}
	if raw, ok := rd.get(KeyCodexLogin); ok {
		if l, err := ParseCodexLogin(raw); err == nil {
			c.Login = &l
		} else {
			slog.Warn("jevtrialsettings: ignoring the stored Codex login record", "error", err)
		}
	}
	return c
}

// ParseCodexEnabled decodes the runtime_settings value of KeyCodexEnabled (the
// JSON integer 0 or 1).
func ParseCodexEnabled(jsonValue string) (bool, error) {
	return parseSwitch(KeyCodexEnabled, jsonValue)
}

func parseSwitch(key, jsonValue string) (bool, error) {
	var n *float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return false, fmt.Errorf("jevtrialsettings: decode runtime setting %s=%q as a number: %w", key, jsonValue, err)
	}
	if n == nil || (*n != 0 && *n != 1) {
		return false, fmt.Errorf("jevtrialsettings: runtime setting %s=%q must be 0 or 1", key, jsonValue)
	}
	return *n == 1, nil
}
