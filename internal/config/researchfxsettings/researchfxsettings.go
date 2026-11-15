// Package researchfxsettings holds the Settings-screen switch and the currency
// pairs of the research-only FX 1-minute candle record (issue #1084): whether the
// daily maintenance task `research_fx` fetches GMO's public FX candles and
// stores them at all, and which pairs.
//
// Both keys live in runtime_settings (docs/architecture/er.md §runtime_settings);
// there is no yaml or environment source. The record writes only research tables
// and never feeds a trading decision, but it is an outbound request series and a
// write the trading process makes, so it is OFF by default: while nothing is
// stored (or the stored value is damaged) the task makes no request and writes
// nothing. The switch (KeyEnabled) is on the Paper results MCP's settings_set
// whitelist (mcpwritesettings) because turning it on or off moves no money; the
// pairs are changed on the Settings screen only.
package researchfxsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// Keys of the runtime_settings rows.
const (
	// KeyEnabled is the switch: the JSON integer 1 (record) or 0 (do not).
	KeyEnabled = "research.fx.enabled"
	// KeySymbols is the pairs to record: a JSON string of comma-separated GMO
	// pair names (e.g. "USD_JPY,EUR_USD"), at most MaxSymbols, no duplicates.
	KeySymbols = "research.fx.symbols"
)

// DefaultEnabled is the switch while nothing is stored: off.
const DefaultEnabled = false

// DefaultSymbolsText is the pairs while nothing is stored.
const DefaultSymbolsText = "USD_JPY"

// MaxSymbols bounds the pairs: every pair costs two requests per trading day.
const MaxSymbols = 5

// symbolPattern is GMO's pair naming (the same shape the adapter validates).
var symbolPattern = regexp.MustCompile(`^[A-Z]{3}_[A-Z]{3}$`)

// Keys returns every runtime_settings key of this package.
func Keys() []string { return []string{KeyEnabled, KeySymbols} }

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool { return key == KeyEnabled || key == KeySymbols }

// Text renders a switch state as stored: "1" or "0".
func Text(enabled bool) string {
	if enabled {
		return "1"
	}
	return "0"
}

// DefaultSymbols returns the default pairs (a new slice).
func DefaultSymbols() []string { return []string{DefaultSymbolsText} }

// ParseEnabled decodes the runtime_settings value of KeyEnabled (the JSON
// integer 0 or 1).
func ParseEnabled(jsonValue string) (bool, error) {
	var n *float64 // a pointer, so JSON null is told from 0
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return false, fmt.Errorf("researchfxsettings: decode runtime setting %s=%q as a number: %w", KeyEnabled, jsonValue, err)
	}
	if n == nil || (*n != 0 && *n != 1) {
		return false, fmt.Errorf("researchfxsettings: runtime setting %s=%q must be 0（無効）または 1（有効）", KeyEnabled, jsonValue)
	}
	return *n == 1, nil
}

// Normalize checks a comma-separated list of GMO pair names and returns it
// canonical: trimmed, in the given order, joined by ",". A blank list, a name
// that is not like USD_JPY, a duplicate, or more than MaxSymbols pairs is an
// error.
func Normalize(raw string) (string, error) {
	symbols, err := split(raw)
	if err != nil {
		return "", err
	}
	return strings.Join(symbols, ","), nil
}

func split(raw string) ([]string, error) {
	var symbols []string
	seen := map[string]bool{}
	for part := range strings.SplitSeq(raw, ",") {
		s := strings.TrimSpace(part)
		switch {
		case !symbolPattern.MatchString(s):
			return nil, fmt.Errorf("%q は通貨ペアの形式（例: USD_JPY）ではありません", s)
		case seen[s]:
			return nil, fmt.Errorf("通貨ペア %s が重複しています", s)
		}
		seen[s] = true
		symbols = append(symbols, s)
	}
	if len(symbols) > MaxSymbols {
		return nil, fmt.Errorf("通貨ペアは %d 件までです（%d 件指定）", MaxSymbols, len(symbols))
	}
	return symbols, nil
}

// ParseSymbols decodes the runtime_settings value of KeySymbols (a JSON string
// of comma-separated pairs) and checks it the way Normalize does.
func ParseSymbols(jsonValue string) ([]string, error) {
	var raw string
	if err := json.Unmarshal([]byte(jsonValue), &raw); err != nil {
		return nil, fmt.Errorf("researchfxsettings: decode runtime setting %s=%q as a string: %w", KeySymbols, jsonValue, err)
	}
	symbols, err := split(raw)
	if err != nil {
		return nil, fmt.Errorf("researchfxsettings: runtime setting %s=%q: %w", KeySymbols, jsonValue, err)
	}
	return symbols, nil
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Settings is the effective state of both keys.
type Settings struct {
	Enabled bool
	Symbols []string
}

// Load reads the effective settings. A key with nothing stored, or a stored row
// that does not parse (the Settings screen and the MCP store only valid ones), is
// logged if damaged and counts as its default, so a fault can neither switch the
// recording on nor change the pairs. Only a failed store read is an error: the
// caller must not guess then.
func Load(ctx context.Context, store Store) (Settings, error) {
	s := Settings{Enabled: DefaultEnabled, Symbols: DefaultSymbols()}
	raw, ok, err := store.Get(ctx, KeyEnabled)
	if err != nil {
		return Settings{}, fmt.Errorf("researchfxsettings: read %s: %w", KeyEnabled, err)
	}
	if ok {
		if on, perr := ParseEnabled(raw); perr != nil {
			slog.Warn("researchfxsettings: ignoring invalid stored setting; the FX record stays off", "error", perr)
		} else {
			s.Enabled = on
		}
	}
	raw, ok, err = store.Get(ctx, KeySymbols)
	if err != nil {
		return Settings{}, fmt.Errorf("researchfxsettings: read %s: %w", KeySymbols, err)
	}
	if ok {
		if symbols, perr := ParseSymbols(raw); perr != nil {
			slog.Warn("researchfxsettings: ignoring invalid stored setting; the default pairs apply", "default", DefaultSymbolsText, "error", perr)
		} else {
			s.Symbols = symbols
		}
	}
	return s, nil
}

// ValidSymbol reports whether s is a GMO pair name such as USD_JPY.
func ValidSymbol(s string) bool { return symbolPattern.MatchString(s) }
