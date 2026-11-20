// Package researchindexsettings holds the Settings-screen switch and the index
// codes of the research-only index record (issue #1084): whether the daily
// maintenance task `research_index` copies the index values that the 立花 quote
// recording (issue #1083) stored into research_index_minutes, and which codes.
//
// Both keys live in runtime_settings (docs/architecture/er.md §runtime_settings);
// there is no yaml or environment source. The task reads local tables only (no
// request to any service), but it is a write the trading process makes, so it is
// OFF by default: while nothing is stored (or the stored value is damaged) the
// task reads and writes nothing. The switch (KeyEnabled) is on the Paper results
// MCP's settings_set whitelist (mcpwritesettings) because turning it on or off
// moves no money; the codes are changed on the Settings screen only.
package researchindexsettings

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
	KeyEnabled = "research.index.enabled"
	// KeySymbols is the index codes to record: a JSON string of comma-separated
	// 立花 codes (e.g. "101,151"), at most MaxSymbols, no duplicates.
	KeySymbols = "research.index.symbols"
)

// DefaultEnabled is the switch while nothing is stored: off.
const DefaultEnabled = false

// DefaultSymbolsText is the codes while nothing is stored: 101 日経平均 and 151 TOPIX.
const DefaultSymbolsText = "101,151"

// MaxSymbols bounds the codes.
const MaxSymbols = 5

// symbolPattern is the shape of a code the 立花 EVENT feed accepts.
var symbolPattern = regexp.MustCompile(`^[0-9A-Za-z]{1,8}$`)

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

// DefaultSymbols returns the default codes (a new slice).
func DefaultSymbols() []string { return strings.Split(DefaultSymbolsText, ",") }

// DefaultText is the stored-form text of key while nothing is stored.
func DefaultText(key string) string {
	if key == KeyEnabled {
		return Text(DefaultEnabled)
	}
	return DefaultSymbolsText
}

// Validate checks one stored value of key (the switch, or the codes); each value
// stands alone.
func Validate(key, jsonValue string) error {
	var err error
	if key == KeyEnabled {
		_, err = ParseEnabled(jsonValue)
	} else {
		_, err = ParseSymbols(jsonValue)
	}
	return err
}

// ParseEnabled decodes the runtime_settings value of KeyEnabled (the JSON
// integer 0 or 1).
func ParseEnabled(jsonValue string) (bool, error) {
	var n *float64 // a pointer, so JSON null is told from 0
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return false, fmt.Errorf("researchindexsettings: decode runtime setting %s=%q as a number: %w", KeyEnabled, jsonValue, err)
	}
	if n == nil || (*n != 0 && *n != 1) {
		return false, fmt.Errorf("researchindexsettings: runtime setting %s=%q must be 0（無効）または 1（有効）", KeyEnabled, jsonValue)
	}
	return *n == 1, nil
}

// Normalize checks a comma-separated list of index codes and returns it
// canonical: trimmed, in the given order, joined by ",". A blank list, a code
// that is not 1 to 8 letters or digits, a duplicate, or more than MaxSymbols
// codes is an error.
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
			return nil, fmt.Errorf("%q は指数コード（英数字 1〜8 文字。例: 101）ではありません", s)
		case seen[s]:
			return nil, fmt.Errorf("指数コード %s が重複しています", s)
		}
		seen[s] = true
		symbols = append(symbols, s)
	}
	if len(symbols) > MaxSymbols {
		return nil, fmt.Errorf("指数コードは %d 件までです（%d 件指定）", MaxSymbols, len(symbols))
	}
	return symbols, nil
}

// ParseSymbols decodes the runtime_settings value of KeySymbols (a JSON string of
// comma-separated codes) and checks it the way Normalize does.
func ParseSymbols(jsonValue string) ([]string, error) {
	var raw string
	if err := json.Unmarshal([]byte(jsonValue), &raw); err != nil {
		return nil, fmt.Errorf("researchindexsettings: decode runtime setting %s=%q as a string: %w", KeySymbols, jsonValue, err)
	}
	symbols, err := split(raw)
	if err != nil {
		return nil, fmt.Errorf("researchindexsettings: runtime setting %s=%q: %w", KeySymbols, jsonValue, err)
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
// record on nor change the codes. Only a failed store read is an error: the
// caller must not guess then.
func Load(ctx context.Context, store Store) (Settings, error) {
	s := Settings{Enabled: DefaultEnabled, Symbols: DefaultSymbols()}
	raw, ok, err := store.Get(ctx, KeyEnabled)
	if err != nil {
		return Settings{}, fmt.Errorf("researchindexsettings: read %s: %w", KeyEnabled, err)
	}
	if ok {
		if on, perr := ParseEnabled(raw); perr != nil {
			slog.Warn("researchindexsettings: ignoring invalid stored setting; the index record stays off", "error", perr)
		} else {
			s.Enabled = on
		}
	}
	raw, ok, err = store.Get(ctx, KeySymbols)
	if err != nil {
		return Settings{}, fmt.Errorf("researchindexsettings: read %s: %w", KeySymbols, err)
	}
	if ok {
		if symbols, perr := ParseSymbols(raw); perr != nil {
			slog.Warn("researchindexsettings: ignoring invalid stored setting; the default codes apply", "default", DefaultSymbolsText, "error", perr)
		} else {
			s.Symbols = symbols
		}
	}
	return s, nil
}

// ValidSymbol reports whether s is an index code such as 101.
func ValidSymbol(s string) bool { return symbolPattern.MatchString(s) }
