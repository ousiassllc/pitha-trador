package trialquery

import "github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"

// CostNotice is the fixed caveat jev_trial_cost returns.
const CostNotice = "費用は出所別に分ける: reported（応答が報告した usage.cost）・list_price_estimate（定価×入力トークンの推定）・unknown（どちらも無い）。" +
	"unknown の実費は分からない（actual_usd は null。0 円ではない）。unknown_counted_usd は 1 日の上限の判定に数えた額（最悪値側の単価で数えた上限の見積り。課金されない前提の 4xx・529 応答は 0 と数えるので、その件数を counted_as_zero_calls に出す）で、reported・list_price_estimate の合計には混ぜない。" +
	"list_price_estimate の usd は定価での推定額で、上限には、処理先を固定していないアーム（provider_only なし）なら定価と最悪値側の単価の高い方で数えた額 counted_usd を数える（低い定価で上限が甘くならないため）。" +
	"見送り（呼ばなかった比較。費用は無く、上の呼び出し数・額には入らない）の今日の件数は limits.skipped_today に理由別に出す（全体とアームごと）。" +
	"counted_total_usd は上限の判定に数えた額の合計（reported.counted_usd + list_price_estimate.counted_usd + unknown_counted_usd）。エラー本文・応答本文・キーは返さない"

// UnknownBasis names what UnknownCost.UnknownCountedUSD is.
const UnknownBasis = "cap_counting_upper_bound"

// SourceCost is the calls of one known cost source, their summed cost and the
// amount the daily caps counted for them. CountedUSD equals USD for reported
// calls; for list_price_estimate it can be higher: the caps count such a call at
// max(list price, WorstCaseUSDPerMtok) unless the arm's route is pinned
// (provider_only), so a low configured list price does not loosen a cap.
type SourceCost struct {
	Calls      int     `json:"calls"`
	USD        float64 `json:"usd"`
	CountedUSD float64 `json:"counted_usd"`
}

// UnknownCost is the calls whose cost is unknown. ActualUSD is always null: the
// real cost is not known (it is not 0). UnknownCountedUSD is what the daily caps
// counted for them (a worst-case price over the request's size), an upper-bound
// counting figure; CountedAsZeroCalls are the unknown calls the caps counted as
// 0 (an HTTP error response, assumed not billed), so a reader cannot take a low
// counted sum for "free".
type UnknownCost struct {
	Calls              int      `json:"calls"`
	ActualUSD          *float64 `json:"actual_usd"`
	UnknownCountedUSD  float64  `json:"unknown_counted_usd"`
	CountedAsZeroCalls int      `json:"counted_as_zero_calls"`
	Basis              string   `json:"basis"`
}

// CostBlock is calls and cost by source. Reported and ListPriceEstimate sum
// cost_usd; Unknown is kept apart and never added to them.
type CostBlock struct {
	Calls             int         `json:"calls"`
	Reported          SourceCost  `json:"reported"`
	ListPriceEstimate SourceCost  `json:"list_price_estimate"`
	Unknown           UnknownCost `json:"unknown"`
	CountedTotalUSD   float64     `json:"counted_total_usd"`
}

func newCostBlock() CostBlock { return CostBlock{Unknown: UnknownCost{Basis: UnknownBasis}} }

// add counts calls rows of source: usd is their summed cost_usd (known sources
// only), counted their summed counted_cost_usd, zeroCounted those counted as 0.
func (b *CostBlock) add(source string, calls int, usd, counted float64, zeroCounted int) {
	b.Calls += calls
	b.CountedTotalUSD += counted
	switch source {
	case jevtrial.CostReported:
		b.Reported.Calls += calls
		b.Reported.USD += usd
		b.Reported.CountedUSD += counted
	case jevtrial.CostListPriceEstimate:
		b.ListPriceEstimate.Calls += calls
		b.ListPriceEstimate.USD += usd
		b.ListPriceEstimate.CountedUSD += counted
	default:
		b.Unknown.Calls += calls
		b.Unknown.UnknownCountedUSD += counted
		b.Unknown.CountedAsZeroCalls += zeroCounted
	}
}

// ArmCost is one arm's cost block.
type ArmCost struct {
	ArmID string `json:"arm_id"`
	CostBlock
}

// DayCost is the cost of one JST day, overall and by arm.
type DayCost struct {
	Date    string    `json:"date"`
	Overall CostBlock `json:"overall"`
	Arms    []ArmCost `json:"arms"`
}

// HaltInfo is a recorded stop of the whole trial or of one arm (reason and day
// only). ActiveToday is true when the record is of today's JST day.
type HaltInfo struct {
	Date        string `json:"date"`
	Reason      string `json:"reason"`
	ActiveToday bool   `json:"active_today"`
}

// ThrottleInfo is an arm's last 429/529 stop (reason and day only); Active is
// true while its cooldown is still running.
type ThrottleInfo struct {
	Date   string `json:"date"`
	Reason string `json:"reason"`
	Active bool   `json:"active"`
}

// ArmLimits is a configured arm's cap and today's state.
type ArmLimits struct {
	ArmID           string  `json:"arm_id"`
	Model           string  `json:"model"`
	PromptVersion   string  `json:"prompt_version"`
	DailyCapUSD     float64 `json:"daily_cap_usd"`
	ListPriceKnown  bool    `json:"list_price_known"`
	CountedTodayUSD float64 `json:"counted_today_usd"`
	// SkippedToday counts today's comparisons of this arm that were not made (by
	// reason, every reason present). A skip has no row and no cost.
	SkippedToday  map[string]int `json:"skipped_today"`
	FailuresToday int            `json:"failures_today"`
	Halt          *HaltInfo      `json:"halt"`
	Throttle      *ThrottleInfo  `json:"throttle"`
}

// Limits is the trial's current caps and stop state, read from runtime_settings
// (read-only: no tool writes any of it).
type Limits struct {
	Enabled bool   `json:"enabled"`
	Today   string `json:"today"`
	// ArmsConfigInvalid is true when the stored arms do not parse (the trial then
	// runs no arm); the parse error is not returned.
	ArmsConfigInvalid      bool    `json:"arms_config_invalid"`
	OverallDailyCapUSD     float64 `json:"overall_daily_cap_usd"`
	OverallCapSource       string  `json:"overall_cap_source"`
	OverallCountedTodayUSD float64 `json:"overall_counted_today_usd"`
	// SkippedToday counts today's comparisons (one per live decision and arm) the
	// trial did not make, over every arm, by reason (every reason present). They cost
	// nothing and are not in the calls or the counted cost above.
	SkippedToday            map[string]int `json:"skipped_today"`
	Halt                    *HaltInfo      `json:"halt"`
	FailureCap              int            `json:"failure_cap"`
	ThrottleCooldownSeconds int            `json:"throttle_cooldown_seconds"`
	MinCallIntervalSeconds  int            `json:"min_call_interval_seconds"`
	WorstCaseUSDPerMtok     float64        `json:"worst_case_usd_per_mtok"`
	Arms                    []ArmLimits    `json:"arms"`
}

// CostReport is jev_trial_cost's data.
type CostReport struct {
	Notice string    `json:"notice"`
	Limits Limits    `json:"limits"`
	Total  CostBlock `json:"total"`
	PerArm []ArmCost `json:"per_arm"`
	Daily  []DayCost `json:"daily"`
}
