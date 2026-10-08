package tachibanasource

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

var runTimePattern = regexp.MustCompile(`^\d{1,2}:\d{2}$`)

var symbolCodePattern = regexp.MustCompile(`^[0-9A-Z]{1,10}$`)

func errNotSourceSetting(key string) error {
	return fmt.Errorf("config: %q is not a 立花 監視銘柄ソース setting", key)
}

func formatSettingNumber(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func parseIntIn(raw string, lo, hi int) (any, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo || n > hi {
		return nil, fmt.Errorf("%d〜%d の整数で指定してください", lo, hi)
	}
	return n, nil
}

func parseFloatIn(raw string, lo, hi float64) (any, error) {
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < lo || f > hi {
		return nil, fmt.Errorf("%s〜%s の数値で指定してください", formatSettingNumber(lo), formatSettingNumber(hi))
	}
	return f, nil
}

// TachibanaNightlyBlocked reports whether the nightly daily-bar batch must
// not start at hhmm ("HH:MM"): 東証の立会・日中の 08:00〜15:30 は全銘柄巡回や
// 日足の一括取得をしない（#720）。
func TachibanaNightlyBlocked(hhmm string) bool {
	return hhmm >= TachibanaNightlyBlockedFrom && hhmm <= TachibanaNightlyBlockedTo
}

// normalizeNightlyRunTime accepts "H:MM"/"HH:MM" outside the blocked window
// and returns it as zero-padded "HH:MM".
func normalizeNightlyRunTime(raw string) (string, error) {
	format := fmt.Sprintf("HH:MM 形式で指定してください（%s〜%s は指定できません。既定は %s）", TachibanaNightlyBlockedFrom, TachibanaNightlyBlockedTo, DefaultTachibanaNightlyRunTime)
	if !runTimePattern.MatchString(raw) {
		return "", errors.New(format)
	}
	t, err := time.Parse("15:04", raw)
	if err != nil {
		return "", errors.New(format)
	}
	v := t.Format("15:04")
	if TachibanaNightlyBlocked(v) {
		return "", fmt.Errorf("%s〜%s は日中のため夜間バッチの実行時刻に指定できません（%s 以降、または翌 01:00 以降を指定してください）", TachibanaNightlyBlockedFrom, TachibanaNightlyBlockedTo, DefaultTachibanaNightlyRunTime)
	}
	return v, nil
}

// splitList splits comma-, whitespace- and 読点-separated text.
func splitList(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '、' || r == '，' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '　'
	})
}

// normalizeSymbolList returns the distinct upper-cased symbols of raw joined
// by ",", at most limit of them.
func normalizeSymbolList(raw string, limit int) (string, error) {
	var out []string
	for _, tok := range splitList(raw) {
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
		return "", errors.New("銘柄コードをカンマ区切りで指定してください")
	case len(out) > limit:
		return "", fmt.Errorf("銘柄は最大 %d 件です（%d 件指定されています）", limit, len(out))
	}
	return strings.Join(out, ","), nil
}

// normalizeMarkets returns the distinct markets of raw in canonical order.
func normalizeMarkets(raw string) (string, error) {
	allowed := strings.Join(TachibanaMarkets, " / ")
	want := map[string]bool{}
	for _, tok := range splitList(raw) {
		m := strings.ToLower(tok)
		if !slices.Contains(TachibanaMarkets, m) {
			return "", fmt.Errorf("市場区分は %s をカンマ区切りで指定してください", allowed)
		}
		want[m] = true
	}
	if len(want) == 0 {
		return "", fmt.Errorf("市場区分は %s をカンマ区切りで指定してください", allowed)
	}
	var out []string
	for _, m := range TachibanaMarkets {
		if want[m] {
			out = append(out, m)
		}
	}
	return strings.Join(out, ","), nil
}

// TachibanaScreenIndicator is one screening indicator: it selects its TopN
// symbols (0 = not used) and Weight orders the union when the slots run out.
type TachibanaScreenIndicator struct {
	Name   string
	Weight float64
	TopN   int
}

// Active reports whether the indicator takes part in the screening.
func (i TachibanaScreenIndicator) Active() bool { return i.TopN > 0 && i.Weight > 0 }

// TachibanaNightlySettings are the nightly daily-bar batch settings.
type TachibanaNightlySettings struct {
	RunTime        string // "HH:MM", never within TachibanaNightlyBlocked
	MaxPerSecond   float64
	Markets        []string // subset of TachibanaMarkets
	MinPriceJPY    int      // 0 = no price floor
	ExcludeSymbols []string
}

// TachibanaRestQuoteSettings bound the REST 時価 assist.
type TachibanaRestQuoteSettings struct {
	MinIntervalSeconds int
	RequestsPerRound   int
}

// TachibanaSourceSettings are the effective 立花 監視銘柄ソース settings
// (issue #728). Later children (#726) read them from the opsettings service.
type TachibanaSourceSettings struct {
	CandidateSource        string // TachibanaSourceDailyScreen | TachibanaSourceFixed
	ManualSymbols          []string
	FixedSymbols           []string
	Nightly                TachibanaNightlySettings
	Screen                 []TachibanaScreenIndicator // in TachibanaScreenIndicators order
	EventMaxConnectsPerDay int
	RestQuote              TachibanaRestQuoteSettings
}

// BuildTachibanaSource assembles the settings from the text of every
// TachibanaSourceSettingKeys key (already normalized; a missing or
// unparsable entry falls back to its default).
func BuildTachibanaSource(text map[string]string) TachibanaSourceSettings {
	get := func(key string) string {
		if v, ok := text[key]; ok {
			return v
		}
		return TachibanaSourceDefault(key)
	}
	num := func(key string) float64 {
		f, err := strconv.ParseFloat(get(key), 64)
		if err != nil {
			f, _ = strconv.ParseFloat(TachibanaSourceDefault(key), 64)
		}
		return f
	}
	integer := func(key string) int { return int(num(key)) }
	list := func(key string) []string { return splitList(get(key)) }

	s := TachibanaSourceSettings{
		CandidateSource: get(KeyTachibanaCandidateSource),
		ManualSymbols:   list(KeyTachibanaManualSymbols),
		FixedSymbols:    list(KeyTachibanaFixedSymbols),
		Nightly: TachibanaNightlySettings{
			RunTime:        get(KeyTachibanaNightlyRunTime),
			MaxPerSecond:   num(KeyTachibanaNightlyRatePerSecond),
			Markets:        list(KeyTachibanaNightlyMarkets),
			MinPriceJPY:    integer(KeyTachibanaNightlyMinPrice),
			ExcludeSymbols: list(KeyTachibanaNightlyExcludeSymbols),
		},
		EventMaxConnectsPerDay: integer(KeyTachibanaEventMaxConnects),
		RestQuote: TachibanaRestQuoteSettings{
			MinIntervalSeconds: integer(KeyTachibanaRestQuoteMinInterval),
			RequestsPerRound:   integer(KeyTachibanaRestQuoteRequestsPerRound),
		},
	}
	for _, ind := range TachibanaScreenIndicators {
		s.Screen = append(s.Screen, TachibanaScreenIndicator{
			Name: ind, Weight: num(TachibanaScreenWeightKey(ind)), TopN: integer(TachibanaScreenTopNKey(ind)),
		})
	}
	return s
}

// Validate checks the cross-key rule: the screening needs at least one
// indicator with a positive weight and a positive top_n.
func (s TachibanaSourceSettings) Validate() error {
	if !slices.ContainsFunc(s.Screen, TachibanaScreenIndicator.Active) {
		return errors.New("スクリーニング指標は、重みが 0 より大きく上位件数が 1 以上のものを少なくとも 1 つ残してください")
	}
	return nil
}

// oneOf returns raw lower-cased if it is one of allowed.
func oneOf(raw string, allowed ...string) (string, error) {
	v := strings.ToLower(raw)
	if !slices.Contains(allowed, v) {
		return "", fmt.Errorf("%s のいずれかを指定してください", strings.Join(allowed, " / "))
	}
	return v, nil
}
