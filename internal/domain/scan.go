package domain

import "time"

// ScreenReason is one reason an instrument did not become a Fast Screener
// candidate in a scan cycle (requirements/functional.md §4.2 FR-FS-1,
// FR-FE-2/FR-FE-5): either a numeric filter it failed, a value the filter
// needs that was missing, or the top-N cut. Code is the stable
// identifier used in the Scanner API/UI.
type ScreenReason uint8

const (
	// Missing-data reasons: the instrument could not be judged on the
	// value. These make ScreenReasons.Status() ScanStatusMissing.
	ScreenReasonNoSnapshot ScreenReason = iota
	// ScreenReasonStaleSnapshot: the latest bar is older than
	// MaxSnapshotAge during a session (issue #685), e.g. one kept from the
	// previous session or from before the symbol joined the ranking watch
	// list. It clears once a fresh market-data bar arrives.
	ScreenReasonStaleSnapshot
	ScreenReasonMissingTurnover
	ScreenReasonMissingSpread
	ScreenReasonMissingVolumeRatio
	ScreenReasonMissingReturn5m
	ScreenReasonMissingRealizedVol

	// Numeric filter failures (config.FastScreenerConfig thresholds).
	ScreenReasonMinPrice
	ScreenReasonMaxPrice
	ScreenReasonMinTurnover
	ScreenReasonMaxSpread
	ScreenReasonMinVolumeRatio
	ScreenReasonMinAbsReturn5m
	ScreenReasonMinRealizedVol

	// Untradable-now reasons (issue #511): the instrument cannot be filled
	// or is unfavourable to enter whatever its numbers say. Reported as
	// excluded (not missing) with kind=threshold in the Scanner API.
	ScreenReasonSpecialQuote // 特別気配
	ScreenReasonLimitUp      // ストップ高
	ScreenReasonLimitDown    // ストップ安

	// ScreenReasonRankedOut: passed every filter but ranked below
	// FastScreenerConfig.TopN by screen_score.
	ScreenReasonRankedOut

	screenReasonCount
)

type screenReasonInfo struct {
	code, label string
	missing     bool
}

var screenReasonInfos = [screenReasonCount]screenReasonInfo{
	ScreenReasonNoSnapshot:         {"no_snapshot", "市況データ未取得（特徴量が未算出）", true},
	ScreenReasonStaleSnapshot:      {"stale_snapshot", "市況データが古い（立会中に最新の足が3分超経過。次の市況取得までJev評価しない）", true},
	ScreenReasonMissingTurnover:    {"missing_turnover", "売買代金の算出に必要な履歴が不足", true},
	ScreenReasonMissingSpread:      {"missing_spread", "板情報なし（スプレッド不明）", true},
	ScreenReasonMissingVolumeRatio: {"missing_volume_ratio", "出来高倍率を算出できない（履歴不足）", true},
	ScreenReasonMissingReturn5m:    {"missing_return_5m", "5分騰落率を算出できない（履歴不足）", true},
	ScreenReasonMissingRealizedVol: {"missing_realized_vol", "実現ボラティリティを算出できない（履歴不足）", true},
	ScreenReasonMinPrice:           {"min_price", "現在値が下限未満（min_price）", false},
	ScreenReasonMaxPrice:           {"max_price", "現在値が上限超過（max_price）", false},
	ScreenReasonMinTurnover:        {"min_turnover_5m_jpy", "5分売買代金が下限未満（min_turnover_5m_jpy）", false},
	ScreenReasonMaxSpread:          {"max_spread_bps", "スプレッドが上限超過（max_spread_bps）", false},
	ScreenReasonMinVolumeRatio:     {"min_volume_ratio", "出来高倍率が下限未満（min_volume_ratio）", false},
	ScreenReasonMinAbsReturn5m:     {"min_abs_return_5m_pct", "5分騰落率の絶対値が下限未満（min_abs_return_5m_pct）", false},
	ScreenReasonMinRealizedVol:     {"min_realized_volatility", "実現ボラティリティが下限未満（min_realized_volatility）", false},
	ScreenReasonSpecialQuote:       {"special_quote", "特別気配（約定不能）", false},
	ScreenReasonLimitUp:            {"limit_up", "ストップ高（約定不能）", false},
	ScreenReasonLimitDown:          {"limit_down", "ストップ安（約定不能）", false},
	ScreenReasonRankedOut:          {"top_n_cutoff", "条件は満たしたが上位N件に入らなかった（top_n）", false},
}

// Code returns the stable reason identifier (e.g. "min_price").
func (r ScreenReason) Code() string { return screenReasonInfos[r].code }

// Label returns the human-readable Japanese label.
func (r ScreenReason) Label() string { return screenReasonInfos[r].label }

// IsMissing reports whether r is a missing-data reason rather than a
// threshold failure.
func (r ScreenReason) IsMissing() bool { return screenReasonInfos[r].missing }

// ScreenReasonFromCode resolves a Code back to its reason.
func ScreenReasonFromCode(code string) (ScreenReason, bool) {
	for r := ScreenReason(0); r < screenReasonCount; r++ {
		if screenReasonInfos[r].code == code {
			return r, true
		}
	}
	return 0, false
}

// AllScreenReasons lists every reason in display order.
func AllScreenReasons() []ScreenReason {
	out := make([]ScreenReason, screenReasonCount)
	for i := range out {
		out[i] = ScreenReason(i)
	}
	return out
}

// ScreenReasons is a compact set of ScreenReason (a bitmask, so retaining
// ~4,000 per-symbol verdicts costs four bytes each). The zero value is the
// empty set: no reason to exclude, i.e. the instrument passed.
type ScreenReasons uint32

// Add returns s with r added.
func (s ScreenReasons) Add(r ScreenReason) ScreenReasons { return s | 1<<r }

// Has reports whether r is in s.
func (s ScreenReasons) Has(r ScreenReason) bool { return s&(1<<r) != 0 }

// List returns the reasons in s in display order.
func (s ScreenReasons) List() []ScreenReason {
	var out []ScreenReason
	for r := ScreenReason(0); r < screenReasonCount; r++ {
		if s.Has(r) {
			out = append(out, r)
		}
	}
	return out
}

// Status classifies s: empty is passed; any missing-data reason is
// missing (the actionable data problem wins even if a threshold also
// failed); otherwise excluded.
func (s ScreenReasons) Status() ScanStatus {
	if s == 0 {
		return ScanStatusPassed
	}
	for r := ScreenReason(0); r < screenReasonCount; r++ {
		if s.Has(r) && r.IsMissing() {
			return ScanStatusMissing
		}
	}
	return ScanStatusExcluded
}

// ScanStatus is an instrument's outcome in the latest scan cycle.
type ScanStatus string

const (
	ScanStatusPassed   ScanStatus = "passed"   // Fast Screener candidate
	ScanStatusExcluded ScanStatus = "excluded" // failed a threshold / top-N
	ScanStatusMissing  ScanStatus = "missing"  // data missing, could not be judged
)

// ScoutOutcome is the Jev Scout result for a candidate in the cycle.
type ScoutOutcome string

const (
	ScoutPassed ScoutOutcome = "passed"
	ScoutFailed ScoutOutcome = "failed"
	ScoutError  ScoutOutcome = "error" // Jev call failed
)

// ScanSymbol is one universe instrument's verdict in a scan cycle.
type ScanSymbol struct {
	InstrumentID int64
	Symbol       string
	Name         string
	Market       string
	Reasons      ScreenReasons
}

// Status is derived from Reasons.
func (s ScanSymbol) Status() ScanStatus { return s.Reasons.Status() }

// ScanFunnel is the per-stage count of a scan cycle: universe -> feature
// computed -> Fast Screener passed -> Scout passed.
type ScanFunnel struct {
	Universe           int
	FeatureComputed    int // instruments with a persisted snapshot
	FastScreenerPassed int // candidates handed to Jev Scout (after top-N)
	ScoutEvaluated     int // candidates Jev Scout has judged (pass or fail) so far
	ScoutPassed        int
}

// ScanCycle is the latest candidate-refresh cycle's per-symbol results
// (issue #303). Symbols is sorted by Symbol and immutable once published:
// readers share it without copying.
type ScanCycle struct {
	StartedAt  time.Time
	FinishedAt time.Time
	Funnel     ScanFunnel
	Symbols    []ScanSymbol
	// Scout holds Jev Scout outcomes by symbol for the cycle's candidates;
	// a candidate without an entry is still pending.
	Scout map[string]ScoutOutcome
}

// Duration is how long the cycle took.
func (c ScanCycle) Duration() time.Duration { return c.FinishedAt.Sub(c.StartedAt) }
