// Package disclosuresrcsettings holds the runtime_settings of the extra
// disclosure / news sources (issue #1096): the EDINET document list and the
// 立花 e支店 news. Both only RECORD into research tables
// (internal/service/disclosuresrc); nothing they record is a trading input.
//
// Everything is OFF by default: while nothing is stored (or the stored value is
// damaged) the sources send no request and write nothing. The keys live in
// runtime_settings only (no yaml or environment source) and none of them is on
// the Paper results MCP's settings_set whitelist (mcpwritesettings): turning a
// source on is done on the Settings screen only, as `jev.trial.enabled` is.
// The MCP reads them (disclosure_sources_status) but cannot write them.
package disclosuresrcsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strconv"
)

// Keys of the runtime_settings rows. Values are JSON integers.
const (
	// KeyEdinetEnabled is the EDINET switch: 1 (record) or 0 (do not).
	KeyEdinetEnabled = "disclosure_src.edinet.enabled"
	// KeyEdinetDailyRequestCap is the most requests the EDINET reader sends in
	// one JST day (a failed request counts too).
	KeyEdinetDailyRequestCap = "disclosure_src.edinet.daily_request_cap"
	// KeyEdinetLookbackDays is how many JST days back (today included) the
	// reader fetches the document list of.
	KeyEdinetLookbackDays = "disclosure_src.edinet.lookback_days"
	// KeyTachibanaNewsEnabled is the 立花 news switch: 1 or 0.
	KeyTachibanaNewsEnabled = "disclosure_src.tachibana_news.enabled"
	// KeyTachibanaNewsDailyRequestCap is the most news requests in one JST day.
	KeyTachibanaNewsDailyRequestCap = "disclosure_src.tachibana_news.daily_request_cap"
	// KeyRetentionDays is how many JST days of recorded rows are kept.
	KeyRetentionDays = "disclosure_src.retention_days"
)

// KeyPrefix starts every key of this package.
const KeyPrefix = "disclosure_src."

// Defaults and bounds.
const (
	DefaultEnabled = false

	DefaultEdinetDailyRequestCap = 30
	DefaultEdinetLookbackDays    = 7
	DefaultTachibanaDailyCap     = 24
	DefaultRetentionDays         = 1095

	MinCap = 1
	MaxCap = 1000

	MinLookbackDays = 1
	MaxLookbackDays = 3650

	MinRetentionDays = 30
	MaxRetentionDays = 3650
)

// Keys returns every runtime_settings key of this package.
func Keys() []string {
	return []string{KeyEdinetEnabled, KeyEdinetDailyRequestCap, KeyEdinetLookbackDays,
		KeyTachibanaNewsEnabled, KeyTachibanaNewsDailyRequestCap, KeyRetentionDays}
}

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool { return slices.Contains(Keys(), key) }

// isSwitch reports whether key is one of the two on/off keys.
func isSwitch(key string) bool {
	return key == KeyEdinetEnabled || key == KeyTachibanaNewsEnabled
}

// bounds returns the accepted range and the default of a numeric key.
func bounds(key string) (lo, hi, def int) {
	switch key {
	case KeyEdinetDailyRequestCap:
		return MinCap, MaxCap, DefaultEdinetDailyRequestCap
	case KeyEdinetLookbackDays:
		return MinLookbackDays, MaxLookbackDays, DefaultEdinetLookbackDays
	case KeyTachibanaNewsDailyRequestCap:
		return MinCap, MaxCap, DefaultTachibanaDailyCap
	default:
		return MinRetentionDays, MaxRetentionDays, DefaultRetentionDays
	}
}

// DefaultText is the value that applies to key while nothing is stored.
func DefaultText(key string) string {
	if isSwitch(key) {
		return Text(DefaultEnabled)
	}
	_, _, def := bounds(key)
	return strconv.Itoa(def)
}

// Text renders a switch state as stored: "1" or "0".
func Text(enabled bool) string {
	if enabled {
		return "1"
	}
	return "0"
}

// Parse decodes and checks the stored JSON value of key: a switch is the
// integer 0 or 1, the others an integer within their bounds. A switch is
// returned as 0 or 1.
func Parse(key, jsonValue string) (int, error) {
	var n *float64 // a pointer, so JSON null is told from 0
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return 0, fmt.Errorf("disclosuresrcsettings: decode runtime setting %s=%q as a number: %w", key, jsonValue, err)
	}
	if n == nil || *n != math.Trunc(*n) || math.IsInf(*n, 0) {
		return 0, fmt.Errorf("disclosuresrcsettings: runtime setting %s=%q must be an integer", key, jsonValue)
	}
	if isSwitch(key) {
		if *n != 0 && *n != 1 {
			return 0, fmt.Errorf("disclosuresrcsettings: runtime setting %s=%q must be 0 or 1", key, jsonValue)
		}
		return int(*n), nil
	}
	lo, hi, _ := bounds(key)
	if *n < float64(lo) || *n > float64(hi) {
		return 0, fmt.Errorf("disclosuresrcsettings: runtime setting %s=%q must be %d to %d", key, jsonValue, lo, hi)
	}
	return int(*n), nil
}

// Validate reports whether jsonValue is acceptable for key.
func Validate(key, jsonValue string) error {
	if !IsKey(key) {
		return fmt.Errorf("disclosuresrcsettings: %q is not a disclosure source setting", key)
	}
	_, err := Parse(key, jsonValue)
	return err
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Settings is the effective state of every key.
type Settings struct {
	EdinetEnabled         bool
	EdinetDailyRequestCap int
	EdinetLookbackDays    int
	TachibanaNewsEnabled  bool
	TachibanaNewsDailyCap int
	RetentionDays         int
}

// Default is the state while nothing is stored: both sources off.
func Default() Settings {
	return Settings{
		EdinetDailyRequestCap: DefaultEdinetDailyRequestCap,
		EdinetLookbackDays:    DefaultEdinetLookbackDays,
		TachibanaNewsDailyCap: DefaultTachibanaDailyCap,
		RetentionDays:         DefaultRetentionDays,
	}
}

// Load reads every key. A row that no longer parses is logged and falls back to
// its default, so a fault can never turn a source on. A failed READ returns the
// error together with the all-off Default: a caller must not record on a state
// it could not read.
func Load(ctx context.Context, store Store) (Settings, error) {
	s := Default()
	get := func(key string) (int, bool, error) {
		raw, ok, err := store.Get(ctx, key)
		if err != nil {
			return 0, false, fmt.Errorf("disclosuresrcsettings: read runtime setting %s: %w", key, err)
		}
		if !ok {
			return 0, false, nil
		}
		n, perr := Parse(key, raw)
		if perr != nil {
			slog.Warn("disclosuresrcsettings: using the default of a damaged setting", "key", key, "error", perr)
			return 0, false, nil
		}
		return n, true, nil
	}
	for _, key := range Keys() {
		n, ok, err := get(key)
		if err != nil {
			return Default(), err
		}
		if !ok {
			continue
		}
		switch key {
		case KeyEdinetEnabled:
			s.EdinetEnabled = n == 1
		case KeyEdinetDailyRequestCap:
			s.EdinetDailyRequestCap = n
		case KeyEdinetLookbackDays:
			s.EdinetLookbackDays = n
		case KeyTachibanaNewsEnabled:
			s.TachibanaNewsEnabled = n == 1
		case KeyTachibanaNewsDailyRequestCap:
			s.TachibanaNewsDailyCap = n
		case KeyRetentionDays:
			s.RetentionDays = n
		}
	}
	return s, nil
}
