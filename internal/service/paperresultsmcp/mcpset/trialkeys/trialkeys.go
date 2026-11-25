// Package trialkeys is the settings_get / settings_set side of the お試し記録's
// six operator keys (jevtrialsettings.OperatorKeys), which are on the
// whitelist (issue #1220) but are not opsettings keys: the Settings screen saves
// them through jevtrial.Service. mcpset routes them here (getSetting,
// saveSetting), so settings_get / settings_set / the audit log treat them like
// every other whitelisted key, and the checks are the screen's own
// (service/jevtrial: ParseArms, ValidateCap, ValidateCodex).
//
// Only these six are handled. The stop, failure, throttle and login records are
// the runner's and are never read or written here. The package does not import
// mcpset, so mcpset and mcpset/jevaudit can both use it.
package trialkeys

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

// Is reports whether key is one of the six operator keys.
func Is(key string) bool {
	for _, k := range jevtrialsettings.OperatorKeys() {
		if k == key {
			return true
		}
	}
	return false
}

// Value reads one of the six keys the way the runner does
// (jevtrialsettings.Load): a stored row that does not parse counts as the default
// (trial off, no arms, the default caps), so a damaged row cannot show the trial
// as on. The text is the one settings_get shows and the audit log records:
// true / false for a switch, the compact JSON array for the arms, the number for a
// cap.
func Value(ctx context.Context, store opsettings.Store, key string) (opsettings.Value, error) {
	raw, stored, err := store.Get(ctx, key)
	if err != nil {
		return opsettings.Value{}, fmt.Errorf("trialkeys: read %s: %w", key, err)
	}
	v := opsettings.Value{Key: key, Overridden: stored, Default: defaultText(key)}
	v.Current = v.Default
	if !stored {
		return v, nil
	}
	cur, perr := parseStored(key, raw)
	if perr != nil {
		v.Warning = "保存された値が壊れているため、既定として扱っています"
		return v, nil
	}
	v.Current = cur
	return v, nil
}

// defaultText is the text of a key's value while nothing is stored.
func defaultText(key string) string {
	switch key {
	case jevtrialsettings.KeyEnabled:
		return strconv.FormatBool(jevtrialsettings.DefaultEnabled)
	case jevtrialsettings.KeyArms:
		return "[]"
	case jevtrialsettings.KeyDailyCostCapUSD:
		return formatUSD(jevtrialsettings.DefaultDailyCostCapUSD)
	case jevtrialsettings.KeyCodexEnabled:
		return "false"
	case jevtrialsettings.KeyCodexDailyCallCap:
		return strconv.Itoa(jevtrialsettings.DefaultCodexDailyCallCap)
	case jevtrialsettings.KeyCodexDailyTokenCap:
		return strconv.Itoa(jevtrialsettings.DefaultCodexDailyTokenCap)
	}
	return ""
}

// parseStored decodes a stored value with the runner's own parsers and renders
// it as Value's text.
func parseStored(key, raw string) (string, error) {
	switch key {
	case jevtrialsettings.KeyEnabled:
		on, err := jevtrialsettings.ParseEnabled(raw)
		return strconv.FormatBool(on), err
	case jevtrialsettings.KeyCodexEnabled:
		on, err := jevtrialsettings.ParseCodexEnabled(raw)
		return strconv.FormatBool(on), err
	case jevtrialsettings.KeyArms:
		arms, err := jevtrialsettings.ParseArms(raw)
		if err != nil {
			return "", err
		}
		return jevtrialsettings.EncodeArms(arms)
	case jevtrialsettings.KeyDailyCostCapUSD:
		usd, err := jevtrialsettings.ParseCap(raw)
		return formatUSD(usd), err
	case jevtrialsettings.KeyCodexDailyCallCap:
		n, err := jevtrialsettings.ParseCodexCap(key, raw, jevtrialsettings.MaxCodexDailyCallCap)
		return strconv.Itoa(n), err
	case jevtrialsettings.KeyCodexDailyTokenCap:
		n, err := jevtrialsettings.ParseCodexCap(key, raw, jevtrialsettings.MaxCodexDailyTokenCap)
		return strconv.Itoa(n), err
	}
	return "", opsettings.ErrUnknownKey
}

func formatUSD(usd float64) string { return strconv.FormatFloat(usd, 'g', -1, 64) }

// Save validates raw with the Settings screen's checks and stores it. A
// rejection is an *opsettings.InvalidValueError (the MCP's invalid_value with
// the reason) and stores nothing. The write is one key at a time, so each check
// reads what the key depends on from store:
//
//   - enabled: true / false (1 / 0). The arms may be empty (the screen allows it).
//   - arms: the JSON array as a whole, checked as the screen's form is
//     (jevtrial.ParseArms: count, fields, distinct ids, registered prompt version);
//     a codex_chatgpt arm needs the Codex switch to be on already.
//   - daily_cost_cap_usd: above 0 and at most the shared maximum. There is no
//     way to remove it here: nothing stored means the default cap.
//   - codex.enabled: turning it off is refused while a codex_chatgpt arm exists.
//   - codex.daily_call_cap / daily_token_cap: integers within the screen's bounds.
func Save(ctx context.Context, store opsettings.Store, key, raw string, now time.Time) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return opsettings.ErrEmptyValue
	}
	switch key {
	case jevtrialsettings.KeyEnabled, jevtrialsettings.KeyCodexEnabled:
		return saveSwitch(ctx, store, key, raw, now)
	case jevtrialsettings.KeyArms:
		arms, encoded, err := jevtrial.ParseArms(raw)
		if err != nil {
			return invalid(err)
		}
		cfg := jevtrialsettings.Load(ctx, store)
		if cfg.ReadErr != nil {
			return fmt.Errorf("trialkeys: read the trial settings: %w", cfg.ReadErr)
		}
		if err := jevtrial.ValidateCodex(jevtrial.Form{CodexEnabled: cfg.Codex.Enabled}, arms); err != nil {
			return invalid(err)
		}
		return store.Set(ctx, key, encoded, now)
	case jevtrialsettings.KeyDailyCostCapUSD:
		usd, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return &opsettings.InvalidValueError{Reason: fmt.Sprintf("%q は数値（USD）として読めません", raw)}
		}
		if err := jevtrial.ValidateCap(usd); err != nil {
			return invalid(err)
		}
		return store.Set(ctx, key, formatUSD(usd), now)
	case jevtrialsettings.KeyCodexDailyCallCap, jevtrialsettings.KeyCodexDailyTokenCap:
		n, err := strconv.Atoi(raw)
		if err != nil {
			return &opsettings.InvalidValueError{Reason: fmt.Sprintf("%q は整数として読めません", raw)}
		}
		f := jevtrial.Form{CodexEnabled: true}
		if key == jevtrialsettings.KeyCodexDailyCallCap {
			f.CodexCallCap = &n
		} else {
			f.CodexTokenCap = &n
		}
		if err := jevtrial.ValidateCodex(f, nil); err != nil {
			return invalid(err)
		}
		return store.Set(ctx, key, strconv.Itoa(n), now)
	}
	return opsettings.ErrUnknownKey
}

// saveSwitch stores the trial switch or the Codex switch. Turning the
// Codex switch off is checked against the stored arms, as the screen checks its
// form: an arm of the ChatGPT subscription needs the provider on.
func saveSwitch(ctx context.Context, store opsettings.Store, key, raw string, now time.Time) error {
	on, ok := parseSwitchText(raw)
	if !ok {
		return &opsettings.InvalidValueError{Reason: fmt.Sprintf("%q は真偽として読めません。true / false（1 / 0 も可）で指定してください", raw)}
	}
	if key == jevtrialsettings.KeyCodexEnabled && !on {
		cfg := jevtrialsettings.Load(ctx, store)
		if cfg.ReadErr != nil {
			return fmt.Errorf("trialkeys: read the trial settings: %w", cfg.ReadErr)
		}
		if err := jevtrial.ValidateCodex(jevtrial.Form{CodexEnabled: false}, cfg.Arms); err != nil {
			return invalid(err)
		}
	}
	return store.Set(ctx, key, jevtrialsettings.Switch(on), now)
}

// invalid turns a jevtrial.ErrInvalidInput into the MCP's invalid_value
// with the screen's reason; any other error is returned as it is.
func invalid(err error) error {
	if errors.Is(err, jevtrial.ErrInvalidInput) {
		return &opsettings.InvalidValueError{Reason: strings.TrimPrefix(err.Error(), jevtrial.ErrInvalidInput.Error()+": ")}
	}
	return err
}

// parseSwitchText reads the text of a switch: true / false or 1 / 0.
func parseSwitchText(s string) (on, ok bool) {
	switch strings.ToLower(s) {
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	}
	return false, false
}
