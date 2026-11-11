// Package watchsettings holds the Settings-screen controls of the kabu
// ranking watch list size and of the 観察用銘柄 (observation-only symbols)
// with the daily Jev cost cap that bounds their calls (issue #1055).
//
// Every key lives in runtime_settings (docs/architecture/er.md
// §runtime_settings); there is no yaml or environment source, so the watch
// size cannot be raised by an environment variable alone. The defaults are
// today's behaviour: the ranking watch list holds MaxKabuWatched symbols, no
// observation symbol exists and the observation cap has nothing to bound.
//
// The watch size can only go DOWN from the kabu default: the 45 is the broker's
// stream cap (kabuステーション registers at most 50 symbols, shared with the
// REST /board and /symbol registrations, 5 of them stay free for those), so a
// value above it is refused. 立花 is not controlled by this key: its list size
// comes from the 立花 監視銘柄ソース settings, which are capped at its EVENT
// subscription limit of 120 (tachibanasource.MaxTachibanaWatchSymbols). Lowering
// the size is how an operator answers 429 / 4001006 of the information API; the
// information API limiter (infolimit, default 8 requests/second, official
// 10) is not touched by any value here.
//
// Nothing here is read by Policy, Risk, order placement or Paper/Live: the
// observation set only decides which symbols are watched and called on Jev
// Scout, and keeps them out of the jev-trader queue (jev.Scout).
package watchsettings

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
)

// Keys of the runtime_settings rows.
const (
	// KeyMaxWatched is the kabu ranking watch list size (a JSON integer,
	// 1..MaxKabuWatched). Nothing stored means DefaultMaxWatched.
	KeyMaxWatched = "scan.ranking_watch.max_symbols"
	// KeyObservationEnabled is the observation switch: the JSON integer 1 (on)
	// or 0 (off). OFF while nothing is stored or the stored value is damaged.
	KeyObservationEnabled = "scan.observation.enabled"
	// KeyObservationSymbols is the observation-only symbols (a JSON string of
	// comma-separated codes, at most MaxObservationSymbols). Nothing stored
	// means none.
	KeyObservationSymbols = "scan.observation.symbols"
	// KeyObservationDailyCostCapUSD is the daily cost cap, in USD per JST day, of
	// the live Jev Scout calls made for observation-only symbols (a JSON
	// number). Nothing stored means DefaultObservationDailyCostCapUSD.
	KeyObservationDailyCostCapUSD = "jev.observation.daily_cost_cap_usd"
	// KeyObservationUsage is the application's record of the day's observation
	// calls (a JSON object, Usage). It is not a setting: the Settings screen
	// does not show it.
	KeyObservationUsage = "jev.observation.usage"
)

// Keys returns the operator-editable keys of this package (not
// KeyObservationUsage, which the application writes).
func Keys() []string {
	return []string{KeyMaxWatched, KeyObservationEnabled, KeyObservationSymbols, KeyObservationDailyCostCapUSD}
}

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool { return slices.Contains(Keys(), key) }

// IsIntegerKey reports whether key's stored value is a JSON integer.
func IsIntegerKey(key string) bool { return key == KeyMaxWatched || key == KeyObservationEnabled }

const (
	// MaxKabuWatched is the most symbols the kabu watch list registers: the
	// kabuステーション API登録銘柄 limit of 50 minus the 5 slots kept for the REST
	// /board and /symbol registrations. It equals kabu.MaxStreamSymbols and
	// rankingwatch.DefaultMaxWatched (a test pins the three together).
	MaxKabuWatched = 45
	// MinWatched is the smallest watch list size.
	MinWatched = 1
	// DefaultMaxWatched is the watch list size while nothing is stored: the
	// kabu stream cap, i.e. today's behaviour.
	DefaultMaxWatched = MaxKabuWatched
	// DefaultObservationEnabled is the observation switch while nothing is
	// stored.
	DefaultObservationEnabled = false
	// MaxObservationSymbols bounds the observation set, so it can never
	// crowd the whole watch list out.
	MaxObservationSymbols = 10
	// DefaultObservationDailyCostCapUSD is the observation cap while nothing is
	// stored: the same 0.10 USD a day as the shadow's
	// (jevshadowsettings.DefaultDailyCostCapUSD). With the defaults there is no
	// observation symbol, so the cap bounds no call.
	DefaultObservationDailyCostCapUSD = 0.10
	// MaxObservationDailyCostCapUSD bounds a stored cap so a typo cannot lift
	// it to an absurd value.
	MaxObservationDailyCostCapUSD = 100.0
	// DailyFailureCap is how many failed observation calls a JST day may have
	// before the observation calls stop for the day: a failed call may be
	// billed without being stored, so it cannot count toward the cost.
	DailyFailureCap = 20
)

// ClampWatched returns n limited to the broker's stream cap streamCap (0: no
// broker cap known) and to MinWatched.
func ClampWatched(n, streamCap int) int {
	if streamCap > 0 {
		n = min(n, streamCap)
	}
	return max(n, MinWatched)
}

// ParseMaxWatched decodes the runtime_settings value of KeyMaxWatched, a JSON
// integer within MinWatched..MaxKabuWatched.
func ParseMaxWatched(jsonValue string) (int, error) {
	n, err := parseInt(KeyMaxWatched, jsonValue)
	if err != nil {
		return 0, err
	}
	if n < MinWatched || n > MaxKabuWatched {
		return 0, fmt.Errorf("watchsettings: runtime setting %s=%q must be %d〜%d "+
			"（kabuステーションの API 登録枠 50 のうち 5 を REST の /board・/symbol 用に空けるため、監視銘柄数は最大 %d 件です）",
			KeyMaxWatched, jsonValue, MinWatched, MaxKabuWatched, MaxKabuWatched)
	}
	return n, nil
}

// ParseEnabled decodes the runtime_settings value of KeyObservationEnabled
// (the JSON integer 0 or 1).
func ParseEnabled(jsonValue string) (bool, error) {
	var n *float64 // a pointer, so JSON null is told from 0
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return false, fmt.Errorf("watchsettings: decode runtime setting %s=%q as a number: %w", KeyObservationEnabled, jsonValue, err)
	}
	if n == nil || (*n != 0 && *n != 1) {
		return false, fmt.Errorf("watchsettings: runtime setting %s=%q must be 0（無効）または 1（有効）", KeyObservationEnabled, jsonValue)
	}
	return *n == 1, nil
}

var symbolCodePattern = regexp.MustCompile(`^[0-9A-Z]{1,10}$`)

// NormalizeSymbols returns the distinct upper-cased symbol codes of raw (comma
// or whitespace separated) joined by ",", at most MaxObservationSymbols.
func NormalizeSymbols(raw string) (string, error) {
	var out []string
	for _, tok := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '，' || r == '、' || r == ' ' || r == '\t' || r == '\n' }) {
		code := strings.ToUpper(tok)
		if !symbolCodePattern.MatchString(code) {
			return "", fmt.Errorf("銘柄コードは英数字 10 文字以内で指定してください（%q）", tok)
		}
		if !slices.Contains(out, code) {
			out = append(out, code)
		}
	}
	switch {
	case len(out) == 0:
		return "", fmt.Errorf("銘柄コードをカンマ区切りで指定してください")
	case len(out) > MaxObservationSymbols:
		return "", fmt.Errorf("観察用銘柄は最大 %d 件です（%d 件指定されています）", MaxObservationSymbols, len(out))
	}
	return strings.Join(out, ","), nil
}

// ParseSymbols decodes the runtime_settings value of KeyObservationSymbols (a
// JSON string) into its symbol codes.
func ParseSymbols(jsonValue string) ([]string, error) {
	var raw string
	if err := json.Unmarshal([]byte(jsonValue), &raw); err != nil {
		return nil, fmt.Errorf("watchsettings: decode runtime setting %s=%q as a string: %w", KeyObservationSymbols, jsonValue, err)
	}
	text, err := NormalizeSymbols(raw)
	if err != nil {
		return nil, fmt.Errorf("watchsettings: runtime setting %s=%q: %w", KeyObservationSymbols, jsonValue, err)
	}
	return strings.Split(text, ","), nil
}

// ValidateCap checks the observation daily cost cap: finite, above 0 and at
// most MaxObservationDailyCostCapUSD. 0 is refused so the cap can never be
// switched off by accident; the way to stop the observation calls is its
// switch.
func ValidateCap(usd float64) error {
	if math.IsNaN(usd) || math.IsInf(usd, 0) || usd <= 0 || usd > MaxObservationDailyCostCapUSD {
		return fmt.Errorf("watchsettings: the observation daily cost cap must be above 0 and at most %g USD", MaxObservationDailyCostCapUSD)
	}
	return nil
}

// ParseCap decodes the runtime_settings value of KeyObservationDailyCostCapUSD.
func ParseCap(jsonValue string) (float64, error) {
	var n float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return 0, fmt.Errorf("watchsettings: decode runtime setting %s=%q as a number: %w", KeyObservationDailyCostCapUSD, jsonValue, err)
	}
	if err := ValidateCap(n); err != nil {
		return 0, fmt.Errorf("watchsettings: runtime setting %s=%q: %w", KeyObservationDailyCostCapUSD, jsonValue, err)
	}
	return n, nil
}

func parseInt(key, jsonValue string) (int, error) {
	var n float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return 0, fmt.Errorf("watchsettings: decode runtime setting %s=%q as a number: %w", key, jsonValue, err)
	}
	if math.IsNaN(n) || n != math.Trunc(n) {
		return 0, fmt.Errorf("watchsettings: runtime setting %s=%q must be a whole number", key, jsonValue)
	}
	return int(n), nil
}
