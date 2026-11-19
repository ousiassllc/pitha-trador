package tachibanasource

import "strconv"

// runtime_settings keys of the 立花 research-only collections (issues #1082,
// #1083): they record 立花 data next to whatever broker is selected and feed no
// trading decision. They share this package's plumbing (kindSource in
// opsettings: JSON scalars, text defaults, validation) and are read through
// BuildTachibanaSource like the 監視銘柄ソース keys, but only a kabu selection
// ever runs them (the 立花 broker has its own nightly batch and EVENT feed, and
// a 認証ID admits one session).
//
// Both switches are OFF by default. The nightly batch's own keys
// (broker.tachibana.nightly.*) also decide the research daily bars' run time,
// rate, 市場区分 and exclusions.
const (
	// KeyResearchBarsEnabled switches the research daily-bar collection:
	// the JSON integer 1 (collect) or 0 (do not).
	KeyResearchBarsEnabled = "research.tachibana_bars.enabled"

	// KeyResearchQuotesEnabled switches the research 時価 recording (1 / 0).
	KeyResearchQuotesEnabled = "research.tachibana_quotes.enabled"
	// KeyResearchQuotesExtraSource decides what is added to the symbols kabu
	// watches: ResearchExtraManual (the list below) or ResearchExtraDailyScreen
	// (the previous day's 日足スクリーニング top).
	KeyResearchQuotesExtraSource = "research.tachibana_quotes.extra_source"
	// KeyResearchQuotesExtraSymbols is the manual list (at most MaxTachibanaWatchSymbols).
	KeyResearchQuotesExtraSymbols = "research.tachibana_quotes.extra_symbols"
	// KeyResearchQuotesMaxSymbols caps kabu's symbols plus the added ones.
	KeyResearchQuotesMaxSymbols = "research.tachibana_quotes.max_symbols"
	// KeyResearchQuotesMaxConnects is the day's budget of EVENT 接続・切断
	// of the research feed (the broker.tachibana.event.* idea, kept apart).
	KeyResearchQuotesMaxConnects = "research.tachibana_quotes.max_connects_per_day"
	// KeyResearchQuotesRestMinInterval is the shortest interval (seconds)
	// between two REST 時価 rounds of the research feed.
	KeyResearchQuotesRestMinInterval = "research.tachibana_quotes.rest_min_interval_seconds"
	// KeyResearchQuotesRetentionDays is how many JST days of research 時価
	// rows are kept; older days are archived locally, then deleted.
	KeyResearchQuotesRetentionDays = "research.tachibana_quotes.retention_days"
)

// Values of KeyResearchQuotesExtraSource.
const (
	ResearchExtraManual      = "manual"
	ResearchExtraDailyScreen = "daily_screen"
)

// Defaults and bounds of the research collections. The research feed is more
// modest than the broker's own (#720: the broker asks to refrain from heavy
// intraday quote fetching): one REST round per 5 minutes by default.
const (
	DefaultResearchQuotesMaxSymbols = MaxTachibanaWatchSymbols
	ResearchQuotesMaxSymbolsMin     = 1

	DefaultResearchQuotesMaxConnects = 10
	DefaultResearchQuotesRestMinInt  = 300

	DefaultResearchQuotesRetentionDays = 90
	ResearchQuotesRetentionDaysMin     = 1
	ResearchQuotesRetentionDaysMax     = 3650
)

// TachibanaResearchSettings are the effective research-collection settings.
type TachibanaResearchSettings struct {
	BarsEnabled bool

	QuotesEnabled         bool
	QuotesExtraSource     string
	QuotesExtraSymbols    []string
	QuotesMaxSymbols      int
	QuotesMaxConnects     int
	QuotesRestMinInterval int // seconds
	QuotesRetentionDays   int
}

func researchSettings() []sourceSetting {
	onOff := func(raw string) (any, error) { return parseIntIn(raw, 0, 1) }
	return []sourceSetting{
		{KeyResearchBarsEnabled, "0", onOff},
		{KeyResearchQuotesEnabled, "0", onOff},
		{KeyResearchQuotesExtraSource, ResearchExtraManual, func(raw string) (any, error) {
			return oneOf(raw, ResearchExtraManual, ResearchExtraDailyScreen)
		}},
		{KeyResearchQuotesExtraSymbols, "", func(raw string) (any, error) { return normalizeSymbolList(raw, MaxTachibanaWatchSymbols) }},
		{KeyResearchQuotesMaxSymbols, strconv.Itoa(DefaultResearchQuotesMaxSymbols), func(raw string) (any, error) {
			return parseIntIn(raw, ResearchQuotesMaxSymbolsMin, MaxTachibanaWatchSymbols)
		}},
		{KeyResearchQuotesMaxConnects, strconv.Itoa(DefaultResearchQuotesMaxConnects), func(raw string) (any, error) {
			return parseIntIn(raw, TachibanaEventMaxConnectsMin, TachibanaEventMaxConnectsMax)
		}},
		{KeyResearchQuotesRestMinInterval, strconv.Itoa(DefaultResearchQuotesRestMinInt), func(raw string) (any, error) {
			return parseIntIn(raw, TachibanaRestQuoteMinIntervalMin, TachibanaRestQuoteMinIntervalMax)
		}},
		{KeyResearchQuotesRetentionDays, strconv.Itoa(DefaultResearchQuotesRetentionDays), func(raw string) (any, error) {
			return parseIntIn(raw, ResearchQuotesRetentionDaysMin, ResearchQuotesRetentionDaysMax)
		}},
	}
}

// buildResearch reads the research settings from the same text map
// BuildTachibanaSource gets.
func buildResearch(get func(string) string, integer func(string) int, list func(string) []string) TachibanaResearchSettings {
	return TachibanaResearchSettings{
		BarsEnabled:           get(KeyResearchBarsEnabled) == "1",
		QuotesEnabled:         get(KeyResearchQuotesEnabled) == "1",
		QuotesExtraSource:     get(KeyResearchQuotesExtraSource),
		QuotesExtraSymbols:    list(KeyResearchQuotesExtraSymbols),
		QuotesMaxSymbols:      integer(KeyResearchQuotesMaxSymbols),
		QuotesMaxConnects:     integer(KeyResearchQuotesMaxConnects),
		QuotesRestMinInterval: integer(KeyResearchQuotesRestMinInterval),
		QuotesRetentionDays:   integer(KeyResearchQuotesRetentionDays),
	}
}
