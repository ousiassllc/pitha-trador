package datasetexport

import "fmt"

// Column is one column of the dataset: the whitelist is Columns, in file order. Nothing
// outside it is ever written.
type Column struct {
	Name    string `json:"name"`
	Source  string `json:"source"`
	Unit    string `json:"unit"`
	Meaning string `json:"meaning"`
}

// offsets are where each group of Columns starts (the row's cells use the same indexes).
type offsets struct {
	decision, bar, hasBar, instrument, gate, calibration, research, counterfactual, shadow, trial, total int
}

// Indexes of the identifier columns (the first columns of every row).
const (
	cDecisionID = iota
	cInstrumentID
	cSymbol
	cDecidedAt
	cDecisionDay
	cQuestionVersion
	cModelID
	cSplit
	identifierColumns
)

// Gate columns (after the group start).
const (
	gHasSignal = iota
	gSignalCount
	gRiskPassed
	gRejectCode
	gOrigin
	gateColumns
)

// Counterfactual columns (after the group start).
const (
	cfHorizon = iota
	cfNetBps
	cfBarrier
	cfSkipKind
	cfCutoffUnverified
	counterfactualColumns
)

// Columns is the whitelist of the dataset, in file order, and off where its groups start.
var Columns, off = buildColumns()

func buildColumns() ([]Column, offsets) {
	var cols []Column
	var o offsets
	add := func(name, source, unit, meaning string) { cols = append(cols, Column{name, source, unit, meaning}) }
	addField := func(f field) { add(f.name, f.source, f.unit, f.meaning) }

	add("decision_id", "jev_decisions.id", "", "判断の ID（1 行 = 1 判断）")
	add("instrument_id", "jev_decisions.instrument_id", "", "銘柄 ID")
	add("symbol", "jev_decisions.symbol", "", "銘柄コード")
	add("decided_at", "jev_decisions.timestamp", "UTC RFC3339（ナノ秒 9 桁固定）", "判断の時刻")
	add("decision_day", "jev_decisions.timestamp", "JST の日付 YYYY-MM-DD", "判断の日（split の境界はこの日で決める）")
	add("question_version", "jev_decisions.question_version", "", "Jev のプロンプトの版")
	add("model_id", "jev_decisions.model_id", "", "応答したモデル")
	add("split", "分割の規則", "train / eval / all", "分割の区分（分割なしは all）")

	o.decision = len(cols)
	for _, f := range decisionFields {
		addField(f)
	}
	o.bar = len(cols)
	for _, f := range barFields {
		addField(f)
	}
	o.hasBar = len(cols)
	add("has_bar", "market_snapshots（instrument_id と timestamp が判断と同じ足）", "0 / 1", "判断の足があるか（0 のとき足の特徴量は空）")
	o.instrument = len(cols)
	for _, f := range instrumentFields {
		addField(f)
	}

	o.gate = len(cols)
	add("has_signal", "trade_signals.jev_decision_id", "0 / 1", "この判断に取引候補（trade_signals）があるか")
	add("signal_count", "trade_signals", "件", "この判断の取引候補の数")
	add("risk_passed", "trade_signals.risk_passed", "0 / 1", "どれかの候補が Policy・Risk を通ったか（エントリーした）")
	add("reject_code", "trade_signals.reject_reason の先頭の語", "固定コード", "最新の候補が通らなかったときの理由の固定コード（自由文は出さない。未知は other）")
	add("origin", "trade_signals と jev_decisions.direction", "entered / risk_rejected / guard_skipped / no_signal / no_direction", "エントリーした判断か、見送りの種類か（calibration_outcome_origins と同じ規則）")

	o.calibration = len(cols)
	for _, h := range CalibrationHorizons {
		add(fmt.Sprintf("calib_h%d_net_bps", h), "calibration_outcomes.net_return_bps（label_version 2）", "bps", fmt.Sprintf("保存済みラベル（トリプルバリア）の %d 分ホライズンのコスト控除後リターン", h))
		add(fmt.Sprintf("calib_h%d_barrier", h), "calibration_outcomes.barrier_hit（label_version 2）", "tp / sl / timeout", fmt.Sprintf("同 %d 分ホライズンで先に触れたバリア", h))
	}

	o.research = len(cols)
	for _, h := range ResearchHorizons {
		add("rh_"+h+"_long_net_bps", "research_horizon_labels.long_net_bps（label_version 1）", "bps", "研究用ラベル（固定ホライズン "+h+"）で LONG したときのコスト控除後リターン")
		add("rh_"+h+"_short_net_bps", "research_horizon_labels.short_net_bps（label_version 1）", "bps", "同 SHORT したときのコスト控除後リターン")
	}

	o.counterfactual = len(cols)
	add("cf_horizon_minutes", "counterfactual_labels.horizon_minutes", "分", "見送り候補の反実仮想ラベルのホライズン（見送り以外は空）")
	add("cf_net_bps", "counterfactual_labels.net_return_bps", "bps", "もしエントリーしていたときのコスト控除後リターン")
	add("cf_barrier", "counterfactual_labels.barrier_hit", "tp / sl / timeout", "同 先に触れたバリア")
	add("cf_skip_kind", "counterfactual_labels.skip_kind", "risk_rejected / guard_skipped / no_signal", "見送りの種類")
	add("cf_cutoff_unverified", "counterfactual_labels.cutoff_unverified", "0 / 1", "記録時の LLM cutoff が未確認だったか（1 は学習・評価に使わない扱い）")

	o.shadow = len(cols)
	for n := 1; n <= MaxShadowVersions; n++ {
		p := fmt.Sprintf("shadow%d_", n)
		slot := fmt.Sprintf("シャドー候補 %d 番目（件数の多い順・同数は版名の昇順。版名は manifest.shadow_versions）", n)
		add(p+"version", "jev_shadow_decisions.prompt_version", "", slot+": 版名")
		add(p+"direction", "jev_shadow_decisions.direction", "LONG / SHORT / NONE", slot+": 方向")
		add(p+"confidence", "jev_shadow_decisions.confidence", "0〜1", slot+": 確信度")
		add(p+"entry_quality", "jev_shadow_decisions.entry_quality", "poor〜exceptional", slot+": エントリーの質")
	}

	o.trial = len(cols)
	for n := 1; n <= MaxTrialArms; n++ {
		p := fmt.Sprintf("trial%d_", n)
		slot := fmt.Sprintf("お試しアーム %d 番目（件数の多い順・同数は arm_id の昇順。arm_id は manifest.trial_arms）", n)
		add(p+"arm_id", "jev_trial_decisions.arm_id", "", slot+": アーム ID")
		add(p+"status", "jev_trial_decisions.status", "ok / failed / refused / partial", slot+": 応答の状態")
		add(p+"direction", "jev_trial_decisions.direction", "LONG / SHORT / NONE", slot+": 方向（失敗・拒否は空）")
		add(p+"confidence", "jev_trial_decisions.confidence", "0〜1", slot+": 確信度")
	}
	o.total = len(cols)
	return cols, o
}

// ColumnNames is the header of every data file.
func ColumnNames() []string {
	names := make([]string, len(Columns))
	for i, c := range Columns {
		names[i] = c.Name
	}
	return names
}

// ExcludedColumns are what the dataset never contains, whatever the source row holds. The
// export selects named columns only, so none of these is read at all.
var ExcludedColumns = []string{
	"bid", "ask", "bid_depth", "ask_depth", "microprice", "raw_data_json", // raw board and quote data
	"price", "vwap", "volume", "turnover", // price and traded-size levels (the features are ratios and bps)
	"response_json", "state_json", "payload_json", // the raw bodies of Jev's request and answer, jobs
	"error_excerpt", "failure_code", "reject_reason", "last_error", // free text and error bodies
	"runtime_settings", "secrets", // settings values, API keys, passwords, tokens
}
