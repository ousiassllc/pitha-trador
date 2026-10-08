// Package tachibanasource holds the runtime_settings of the 立花 監視銘柄ソース
// (issue #728): keys, defaults, validation and the typed settings the later
// children of #726 read. It lives apart from internal/config to keep that
// directory within the linterly line budget.
package tachibanasource

import (
	"slices"
	"strconv"
	"strings"
)

// runtime_settings keys of the 立花 監視銘柄ソース (issue #728, child of
// #726; docs/architecture/er/tables-system.md §runtime_settings). They are
// the single place the candidate source, the nightly daily-bar batch, the
// screening, the EVENT connection budget and the REST quote assist read
// their tuning from; no environment variable is involved. Values are JSON
// scalars (strings, integers or numbers); a missing row means the default
// below applies, and the lists are comma-separated text.
const (
	// KeyTachibanaCandidateSource selects how tomorrow's 監視銘柄 are
	// decided: TachibanaSourceDailyScreen (default) or TachibanaSourceFixed.
	KeyTachibanaCandidateSource = "broker.tachibana.candidate_source"
	// KeyTachibanaManualSymbols are symbols always added to the 監視リスト
	// in daily_screen mode (at most MaxTachibanaWatchSymbols).
	KeyTachibanaManualSymbols = "broker.tachibana.manual_symbols"
	// KeyTachibanaFixedSymbols is the 監視リスト itself in fixed mode (at most
	// MaxTachibanaWatchSymbols); also the fallback when daily bars failed.
	KeyTachibanaFixedSymbols = "broker.tachibana.fixed_symbols"

	// KeyTachibanaNightlyRunTime is the earliest time of day ("HH:MM", JST)
	// the nightly daily-bar batch starts. It is never within
	// TachibanaNightlyBlockedFrom..TachibanaNightlyBlockedTo.
	KeyTachibanaNightlyRunTime = "broker.tachibana.nightly.run_time"
	// KeyTachibanaNightlyRatePerSecond caps the batch's requests per second.
	KeyTachibanaNightlyRatePerSecond = "broker.tachibana.nightly.max_per_second"
	// KeyTachibanaNightlyMarkets is the universe's 市場区分 (TachibanaMarkets).
	KeyTachibanaNightlyMarkets = "broker.tachibana.nightly.markets"
	// KeyTachibanaNightlyMinPrice excludes symbols priced below it (0 = off).
	KeyTachibanaNightlyMinPrice = "broker.tachibana.nightly.min_price_jpy"
	// KeyTachibanaNightlyExcludeSymbols are symbols left out of the universe.
	KeyTachibanaNightlyExcludeSymbols = "broker.tachibana.nightly.exclude_symbols"

	// KeyTachibanaEventMaxConnects is the day's budget of EVENT 接続・切断.
	KeyTachibanaEventMaxConnects = "broker.tachibana.event.max_connects_per_day"
	// KeyTachibanaRestQuoteMinInterval is the shortest interval (seconds)
	// between two REST 時価 (CLMMfdsGetMarketPrice) rounds.
	KeyTachibanaRestQuoteMinInterval = "broker.tachibana.rest_quote.min_interval_seconds"
	// KeyTachibanaRestQuoteRequestsPerRound is the number of REST 時価
	// requests one round may send.
	KeyTachibanaRestQuoteRequestsPerRound = "broker.tachibana.rest_quote.requests_per_round"
)

// Values of KeyTachibanaCandidateSource.
const (
	TachibanaSourceDailyScreen = "daily_screen"
	TachibanaSourceFixed       = "fixed"
)

// Screening indicators (指標), kabu `/ranking` の種別に寄せたもの. Each has a
// `broker.tachibana.screen.<indicator>.weight` and `.top_n` key.
const (
	ScreenGainRate      = "gain_rate"      // 前日の値上がり率
	ScreenLossRate      = "loss_rate"      // 前日の値下がり率
	ScreenVolume        = "volume"         // 売買高
	ScreenTurnover      = "turnover"       // 売買代金
	ScreenVolumeSurge   = "volume_surge"   // 出来高急増（過去20営業日平均との比）
	ScreenTurnoverSurge = "turnover_surge" // 売買代金急増（同上）
	ScreenRangeRate     = "range_rate"     // 値幅率
)

// Markets (市場区分) of the nightly universe.
const (
	MarketPrime    = "prime"
	MarketStandard = "standard"
	MarketGrowth   = "growth"
	MarketOther    = "other"
)

// Defaults and bounds. The nightly rate and the REST assist are deliberately
// modest (#720: the broker asks to refrain from heavy, frequent quote
// fetching); the real numbers are tuned after the live measurement of #725.
const (
	// MaxTachibanaWatchSymbols is the EVENT I/F's simultaneous symbol limit.
	MaxTachibanaWatchSymbols = 120
	// MaxTachibanaExcludeSymbols bounds the nightly exclusion list.
	MaxTachibanaExcludeSymbols = 500

	// The nightly batch never starts from TachibanaNightlyBlockedFrom up to
	// and including TachibanaNightlyBlockedTo (the 立会・日中の時間帯, #720).
	TachibanaNightlyBlockedFrom    = "08:00"
	TachibanaNightlyBlockedTo      = "15:30"
	DefaultTachibanaNightlyRunTime = "18:00"

	DefaultTachibanaNightlyRatePerSecond = 1.0
	TachibanaNightlyRatePerSecondMin     = 0.1
	TachibanaNightlyRatePerSecondMax     = 3.0

	DefaultTachibanaNightlyMarkets  = "prime,standard,growth"
	DefaultTachibanaNightlyMinPrice = 0
	TachibanaNightlyMinPriceMax     = 1_000_000

	DefaultTachibanaScreenWeight = 1.0
	TachibanaScreenWeightMax     = 100.0
	DefaultTachibanaScreenTopN   = 10
	TachibanaScreenTopNMax       = MaxTachibanaWatchSymbols

	DefaultTachibanaEventMaxConnects = 10
	TachibanaEventMaxConnectsMin     = 1
	TachibanaEventMaxConnectsMax     = 20

	DefaultTachibanaRestQuoteMinInterval = 60
	TachibanaRestQuoteMinIntervalMin     = 10
	TachibanaRestQuoteMinIntervalMax     = 3600
	DefaultTachibanaRestQuoteRequests    = 1
	TachibanaRestQuoteRequestsMin        = 1
	TachibanaRestQuoteRequestsMax        = 3
)

// TachibanaScreenIndicators are the screening indicators in display order.
var TachibanaScreenIndicators = []string{
	ScreenGainRate, ScreenLossRate, ScreenVolume, ScreenTurnover,
	ScreenVolumeSurge, ScreenTurnoverSurge, ScreenRangeRate,
}

// TachibanaMarkets are the allowed values of KeyTachibanaNightlyMarkets in
// canonical order.
var TachibanaMarkets = []string{MarketPrime, MarketStandard, MarketGrowth, MarketOther}

// TachibanaScreenWeightKey / TachibanaScreenTopNKey build an indicator's keys.
func TachibanaScreenWeightKey(indicator string) string {
	return "broker.tachibana.screen." + indicator + ".weight"
}

// TachibanaScreenTopNKey is the indicator's selection size (0 = not used).
func TachibanaScreenTopNKey(indicator string) string {
	return "broker.tachibana.screen." + indicator + ".top_n"
}

// sourceSetting is one 監視銘柄ソース key: its default text and the
// normalizer that validates a trimmed non-empty raw value and returns the
// JSON scalar to store.
type sourceSetting struct {
	key       string
	def       string
	normalize func(raw string) (any, error)
}

var tachibanaSourceSettings = buildTachibanaSourceSettings()

func buildTachibanaSourceSettings() []sourceSetting {
	s := []sourceSetting{
		{KeyTachibanaCandidateSource, TachibanaSourceDailyScreen, func(raw string) (any, error) {
			return oneOf(raw, TachibanaSourceDailyScreen, TachibanaSourceFixed)
		}},
		{KeyTachibanaManualSymbols, "", func(raw string) (any, error) { return normalizeSymbolList(raw, MaxTachibanaWatchSymbols) }},
		{KeyTachibanaFixedSymbols, "", func(raw string) (any, error) { return normalizeSymbolList(raw, MaxTachibanaWatchSymbols) }},
		{KeyTachibanaNightlyRunTime, DefaultTachibanaNightlyRunTime, func(raw string) (any, error) { return normalizeNightlyRunTime(raw) }},
		{KeyTachibanaNightlyRatePerSecond, formatSettingNumber(DefaultTachibanaNightlyRatePerSecond), func(raw string) (any, error) {
			return parseFloatIn(raw, TachibanaNightlyRatePerSecondMin, TachibanaNightlyRatePerSecondMax)
		}},
		{KeyTachibanaNightlyMarkets, DefaultTachibanaNightlyMarkets, func(raw string) (any, error) { return normalizeMarkets(raw) }},
		{KeyTachibanaNightlyMinPrice, strconv.Itoa(DefaultTachibanaNightlyMinPrice), func(raw string) (any, error) {
			return parseIntIn(raw, 0, TachibanaNightlyMinPriceMax)
		}},
		{KeyTachibanaNightlyExcludeSymbols, "", func(raw string) (any, error) { return normalizeSymbolList(raw, MaxTachibanaExcludeSymbols) }},
	}
	for _, ind := range TachibanaScreenIndicators {
		s = append(s,
			sourceSetting{TachibanaScreenWeightKey(ind), formatSettingNumber(DefaultTachibanaScreenWeight), func(raw string) (any, error) {
				return parseFloatIn(raw, 0, TachibanaScreenWeightMax)
			}},
			sourceSetting{TachibanaScreenTopNKey(ind), strconv.Itoa(DefaultTachibanaScreenTopN), func(raw string) (any, error) {
				return parseIntIn(raw, 0, TachibanaScreenTopNMax)
			}},
		)
	}
	return append(s,
		sourceSetting{KeyTachibanaEventMaxConnects, strconv.Itoa(DefaultTachibanaEventMaxConnects), func(raw string) (any, error) {
			return parseIntIn(raw, TachibanaEventMaxConnectsMin, TachibanaEventMaxConnectsMax)
		}},
		sourceSetting{KeyTachibanaRestQuoteMinInterval, strconv.Itoa(DefaultTachibanaRestQuoteMinInterval), func(raw string) (any, error) {
			return parseIntIn(raw, TachibanaRestQuoteMinIntervalMin, TachibanaRestQuoteMinIntervalMax)
		}},
		sourceSetting{KeyTachibanaRestQuoteRequestsPerRound, strconv.Itoa(DefaultTachibanaRestQuoteRequests), func(raw string) (any, error) {
			return parseIntIn(raw, TachibanaRestQuoteRequestsMin, TachibanaRestQuoteRequestsMax)
		}},
	)
}

func findSourceSetting(key string) (sourceSetting, bool) {
	i := slices.IndexFunc(tachibanaSourceSettings, func(s sourceSetting) bool { return s.key == key })
	if i < 0 {
		return sourceSetting{}, false
	}
	return tachibanaSourceSettings[i], true
}

// TachibanaSourceSettingKeys returns every 監視銘柄ソース runtime_settings
// key, in display order.
func TachibanaSourceSettingKeys() []string {
	keys := make([]string, len(tachibanaSourceSettings))
	for i, s := range tachibanaSourceSettings {
		keys[i] = s.key
	}
	return keys
}

// IsTachibanaSourceSettingKey reports whether key is one of
// TachibanaSourceSettingKeys.
func IsTachibanaSourceSettingKey(key string) bool {
	_, ok := findSourceSetting(key)
	return ok
}

// TachibanaSourceDefault is the text that applies to key while nothing is
// stored ("" for the symbol lists).
func TachibanaSourceDefault(key string) string {
	s, _ := findSourceSetting(key)
	return s.def
}

// NormalizeTachibanaSourceSetting validates raw (trimmed, non-empty) for key
// and returns the value to store as a JSON scalar: a string, an int or a
// float64. The error message is safe to show.
func NormalizeTachibanaSourceSetting(key, raw string) (any, error) {
	s, ok := findSourceSetting(key)
	if !ok {
		return nil, errNotSourceSetting(key)
	}
	return s.normalize(strings.TrimSpace(raw))
}
