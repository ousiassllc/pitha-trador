// Package indexdiag is the read-only analysis behind the Paper results MCP's
// index_series_summary tool and the index join of jev_evaluation (issue #1084,
// docs/api/endpoints/research-tools/paper-results-mcp-research-index.md): what the
// research index record holds per index code (count, first and last minute, the
// trading days recorded / unavailable / missing, interior gaps in session
// minutes), and the at-or-before join of those values to decision times.
//
// It reads research_index_minutes, research_index_days, a count of
// research_quote_samples rows and the two research.index.* runtime_settings keys
// through the read-only connection and writes nothing. Its numbers are descriptive
// research output, never an input of trading.
package indexdiag

import "time"

// Limits of one call.
const (
	// MaxGapRuns is the most interior-gap runs listed per series; the totals
	// (runs, missing_minutes) always cover every run.
	MaxGapRuns = 50
	// MaxSymbolLen is the longest index code.
	MaxSymbolLen = 8
)

// Notice is returned with every answer.
const Notice = "研究専用の記録の集計であり、取引の入力ではない。Feature Engine・Policy・Risk・発注・Paper ガード・Live・Jev のプロンプト・メタモデルの学習はこの値を読まない。" +
	"立花証券 e支店の時価（研究用の時価の記録）から写した値で、研究用にのみ使い、再配布しない"

// Definitions are the terms of the answer.
var Definitions = map[string]string{
	"trading_day":        "東証の立会日（JST の日付）。期間（from, to）はこの日付で絞る。為替と違い、06:00 の切り替えは無い",
	"minute":             "立会の 1 分（09:00〜11:30、12:30〜15:30 JST の 330 分）。値は立花の時価の記録がその分の 5 秒目に取った値で、その分の始値でも終値でもない。open_time（UTC）はその分の開始時刻",
	"recorded_days":      "値を 1 件以上記録した立会日の数",
	"unavailable_days":   "確認したが、立花の時価の記録に指数の値が 1 件も無かった立会日。取れていない（立花の記録がオフ・指数コードが購読の対象でない・立花が値を返さなかったなど）。市場休みではなく、欠けとも区別する",
	"missing_days":       "記録を始めた立会日（最初の印の日）以降で、立会日が終わっているのに印の無い日。まだ確認していない・確認に失敗した・取得期間（直近の数立会日）を過ぎて確認できなかった日で、欠け",
	"market_holidays":    "土日・祝日・年末年始など立会の無い日は系列に現れず、欠けにも数えない",
	"interior_gaps":      "値のある立会日の中で、その日の最初の値と最後の値の間に、立会の 1 分で値が 1 件も無い分の連続（start〜end の分数）。昼休み（11:30〜12:30）は数えない。日の始まりが遅い・終わりが早いのは含まない",
	"source_samples":     "期間内に立花の時価の記録（research_quote_samples）に残っている、その指数コードの行数。これが 0 なら、立花の記録に指数が入っていない（追加銘柄に指数コードを入れていない・記録がオフ）",
	"before_recording":   "最初の印の日より前の立会日は、記録を始める前なので欠けに数えない",
	"in_progress_day":    "今日の立会日は、15:35 JST を過ぎるまで終わっていないので、欠けにも数えない",
	"series_not_started": "設定の指数コードで、印がまだ 1 つも無いものは、件数 0・日数 0 で返す",
}

// Recording is the recorder's state, read from runtime_settings.
type Recording struct {
	Enabled      bool     `json:"enabled"`
	Symbols      []string `json:"symbols"`
	LookbackDays int      `json:"lookback_days"`
}

// Days counts the trading days of a series in the period.
type Days struct {
	Recorded        int      `json:"recorded"`
	Unavailable     int      `json:"unavailable"`
	Missing         int      `json:"missing"`
	UnavailableDays []string `json:"unavailable_days"`
	MissingDays     []string `json:"missing_days"`
	// FirstMarkedDay is the first day the series has a marker (over all time:
	// before it, days are not missing); null without a marker.
	FirstMarkedDay *string `json:"first_marked_day"`
	// RecordedThrough is the latest marked trading day in the period.
	RecordedThrough *string `json:"recorded_through"`
}

// GapRun is one run of consecutive session minutes without a value inside a recorded day.
type GapRun struct {
	TradingDay string `json:"trading_day"`
	Start      string `json:"start"`
	End        string `json:"end"`
	Minutes    int    `json:"minutes"`
}

// InteriorGaps lists the gap runs (at most MaxGapRuns, oldest first); Runs and
// MissingMinutes cover all of them.
type InteriorGaps struct {
	Runs           int      `json:"runs"`
	MissingMinutes int      `json:"missing_minutes"`
	List           []GapRun `json:"list"`
	Truncated      bool     `json:"truncated"`
}

// Series is one index code's record in the period.
type Series struct {
	Symbol        string       `json:"symbol"`
	Name          string       `json:"name"`
	Configured    bool         `json:"configured"`
	Minutes       int          `json:"minutes"`
	FirstOpenTime *string      `json:"first_open_time"`
	LastOpenTime  *string      `json:"last_open_time"`
	SourceSamples int          `json:"source_samples"`
	Days          Days         `json:"days"`
	InteriorGaps  InteriorGaps `json:"interior_gaps"`
}

// Data is index_series_summary's data.
type Data struct {
	From        string            `json:"from"`
	To          string            `json:"to"`
	Notice      string            `json:"notice"`
	Recording   Recording         `json:"recording"`
	Series      []Series          `json:"series"`
	Definitions map[string]string `json:"definitions"`
}

// minute is the value width.
const minute = time.Minute
