package trenddiag

import "github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"

// LabelSettings are the label settings a day was evaluated with (the barrier settings in
// force when it was recorded).
type LabelSettings struct {
	HorizonMinutes int     `json:"horizon_minutes"`
	TakeProfitBps  float64 `json:"take_profit_bps"`
	StopLossBps    float64 `json:"stop_loss_bps"`
	BarrierCostBps float64 `json:"barrier_cost_bps"`
}

// DayRow is one recorded JST day of a series. The means are the day's sum over the
// evaluated decisions divided by Evaluated (net bps, after the label's cost), null while
// Evaluated is 0.
type DayRow struct {
	Day       string `json:"day"`
	Decisions int    `json:"decisions"`
	Evaluated int    `json:"evaluated"`
	Excluded  int    `json:"excluded"`
	Entered   int    `json:"entered"`
	// WinRate is the share of evaluated decisions whose llm net is above 0.
	WinRate               *float64      `json:"win_rate"`
	LLMMeanNetBps         *float64      `json:"llm_mean_net_bps"`
	RandomMeanNetBps      *float64      `json:"random_mean_net_bps"`
	MomentumMeanNetBps    *float64      `json:"momentum_mean_net_bps"`
	ContrarianMeanNetBps  *float64      `json:"contrarian_mean_net_bps"`
	LLMMinusRandomMeanBps *float64      `json:"llm_minus_random_mean_bps"`
	LabelSettings         LabelSettings `json:"label_settings"`
}

// Cumulative is the period's statistics built from the day rows: the llm arm's net bps and
// llm minus random (a day's cluster is the sum over its evaluated decisions), with the
// day-cluster bootstrap interval of jev_evaluation.
type Cumulative struct {
	LLM            evaldiag.Metrics `json:"llm"`
	LLMMinusRandom evaldiag.Diff    `json:"llm_minus_random"`
}

// Series is one (model_id, question_version, horizon_minutes) over the period's recorded
// days. LabelSettingsChanged is true when two consecutive recorded days were evaluated
// with different label settings (LabelSettingsChangedOn lists the days on which they
// differ from the previous recorded day); the cumulative statistics then mix labels.
type Series struct {
	ModelID                string     `json:"model_id"`
	QuestionVersion        string     `json:"question_version"`
	HorizonMinutes         int        `json:"horizon_minutes"`
	FirstDay               string     `json:"first_day"`
	LastDay                string     `json:"last_day"`
	DaysRecorded           int        `json:"days_recorded"`
	Decisions              int        `json:"decisions"`
	Evaluated              int        `json:"evaluated"`
	Excluded               int        `json:"excluded"`
	LabelSettingsChanged   bool       `json:"label_settings_changed"`
	LabelSettingsChangedOn []string   `json:"label_settings_changed_on"`
	Cumulative             Cumulative `json:"cumulative"`
	Daily                  []DayRow   `json:"daily"`
}

// Recording says what the record holds overall (not only the period) and how it is
// configured now. The first and last recorded day are null while nothing is recorded.
type Recording struct {
	Enabled          bool    `json:"enabled"`
	RetentionDays    int     `json:"retention_days"`
	FirstRecordedDay *string `json:"first_recorded_day"`
	LastRecordedDay  *string `json:"last_recorded_day"`
	DaysRecorded     int     `json:"days_recorded"`
}

// Data is jev_evaluation_trend's data.
type Data struct {
	From           string            `json:"from"`
	To             string            `json:"to"`
	HorizonMinutes *int              `json:"horizon_minutes,omitempty"`
	PromptVersion  string            `json:"prompt_version,omitempty"`
	ModelID        string            `json:"model_id,omitempty"`
	Notice         string            `json:"notice"`
	Recording      Recording         `json:"recording"`
	Series         []Series          `json:"series"`
	Definitions    map[string]string `json:"definitions"`
	Caveats        []string          `json:"caveats"`
}
