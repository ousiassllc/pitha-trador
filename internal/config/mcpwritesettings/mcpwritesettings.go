// Package mcpwritesettings defines what the Paper results MCP may change
// (issues #806, #807): the whitelist of runtime_settings keys its settings
// tools can write, the switch that turns those writes on and off, and the
// masking rule for audit and tool output.
//
// The whitelist is the only way a key becomes writable from the MCP; a key not
// listed here is rejected before anything is read or stored. It
// deliberately holds only Paper and operating-capital values: the Paper capital and
// exposure limits (papersettings), the per-asset-class budgets
// (bucketsettings), the daily entry-halt thresholds
// (dailyguardsettings), the Paper-only minimum entry quality
// (paperpolicysettings, issue #861), the LLM cutoff with its provenance
// (metalabelsettings) and the Jev shadow's switch and candidate version
// (jevshadowsettings.KeyEnabled / KeyCandidateVersion, issue #929: exactly
// those two; the shadow's cost cap, failure cap, halt and throttle records and
// the live prompt version are not on it) and the research horizon labels'
// recording switch (researchlabelsettings.KeyEnabled, issue #1073: only the
// switch; its retention is not on it), the research FX candle record's
// recording switch (researchfxsettings.KeyEnabled, issue #1084: only the
// switch; its pairs are not on it), the research index record's recording switch
// (researchindexsettings.KeyEnabled, issue #1084: only the switch; its codes are
// not on it) and the on/off switches of the research
// daily evaluation record, the research model training and the training-dataset
// export (researchjobsettings, issues #1089, #1092, #1094: only the switches; the
// retentions and the promotion criteria are not on it) and the お試し記録
// (jevtrialsettings, issue #1220: exactly the six operator keys,
// jevtrialsettings.OperatorKeys - switch, arms, overall daily cost cap, Codex
// switch and its two caps; the stop, failure, throttle and login records are the
// runner's and are not on it. The trial costs money, so this reverses the earlier
// "turned on from the Settings screen only" rule: the write switch below and the
// trial's own caps still apply). It never holds a Live value
// (policy.long|short.min_entry_quality, the Live thresholds, are not on it),
// the Paper bootstrap switch, a secret, or anything that orders, moves money or
// releases the Kill Switch - those have no key in runtime_settings the MCP
// could reach through this list.
//
// The write switch lives in runtime_settings under KeyWriteEnabled so the
// application (the Settings screen) and the MCP process, which share the
// database but not memory, read and change one value. Nothing stored means
// ON: an MCP that listens at all has an API key, and #806 makes writes
// default-on for it.
package mcpwritesettings

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/paperpolicysettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/researchfxsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/researchindexsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/researchlabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
)

// KeyWriteEnabled is the runtime_settings key of the write switch: the JSON
// integer 1 (writes allowed) or 0 (settings_set is refused). It is not one of
// Keys: no settings tool can set it as a setting, only the dedicated switch
// tools and the Settings screen toggle change it.
const KeyWriteEnabled = "mcp.settings_write_enabled"

// DefaultWriteEnabled is the value while nothing is stored.
const DefaultWriteEnabled = true

// KeyOrderProposalsEnabled is the runtime_settings key of the order-proposal
// switch (issue #827): the JSON integer 1 (order_proposal_submit accepts
// proposals) or 0. Unlike the write switch it is OFF while nothing is stored
// and while the stored value is damaged. Only the Settings screen turns it on;
// the MCP can only turn it off (order_proposals_disable). It is not one of
// Keys, so settings_set cannot write it either.
const KeyOrderProposalsEnabled = "mcp.order_proposals_enabled"

// DefaultOrderProposalsEnabled is the value while nothing is stored.
const DefaultOrderProposalsEnabled = false

// Units of a Spec.
const (
	UnitYen     = "yen"
	UnitPercent = "percent"
	UnitCount   = "count"
	UnitDate    = "date"
	UnitText    = "text"
)

// Masked replaces the value of a secret-class key in the audit log and in
// every tool output.
const Masked = "****"

// Spec describes one writable key.
type Spec struct {
	Key string
	// Unit is one of the Unit constants.
	Unit string
	// Description says what the key is, in operator-facing words.
	Description string
	// Constraint states the accepted values (range or format). The write is
	// validated by the same code as the Settings screen; this text only tells
	// a client what to send.
	Constraint string
	// Secret marks a secret-class key: its values are masked. No current key
	// is secret (TestWhitelistHoldsNoSecret pins that); the field makes a
	// future secret key masked by default.
	Secret bool
}

// entryQualityConstraint is the accepted values of both Paper entry-quality keys.
const entryQualityConstraint = "poor / fair / good / strong / exceptional のいずれか（弱い順）"

// bucketConstraint is the accepted range of both bucket budgets.
const bucketConstraint = "0 以上 1 兆円以下の整数。株と債券ETFの合計が運用資金（risk.paper.initial_capital_jpy）以下"

// researchLabelEnabledConstraint is the accepted values of the research label
// recording switch (the opsettings integer 0 / 1).
const researchLabelEnabledConstraint = "1（オン）または 0（オフ）"

// researchSwitchConstraint is the accepted values of the two 立花 research
// collection switches (issues #1082, #1083; the opsettings integer 0 / 1).
const researchSwitchConstraint = "1（オン）または 0（オフ）"

// The accepted values of the two Jev shadow keys (issue #929). The write is
// validated by mcpset with the Settings screen's own check
// (jevprompt.ValidateCandidate); this text only tells a client what to send.
const (
	shadowEnabledConstraint   = "true / false（1 / 0 も可）。true にできるのは候補の版（jev.shadow.candidate_version）が設定済みのときだけ"
	shadowCandidateConstraint = "このビルドに登録されている版（jev_shadow_cost の registered_versions）のうち、ライブの版と違うもの"
)

// The accepted values of the six お試し記録 keys (issue #1220). The writes are
// validated by mcpset with the Settings screen's own checks (service/jevtrial);
// these texts only tell a client what to send.
var (
	trialEnabledConstraint = "true / false（1 / 0 も可）。アームが空でもオンにできる（その間は何も呼ばない）"
	trialSwitchConstraint  = "true / false（1 / 0 も可）。オフにできるのは codex_chatgpt のアームが無いときだけ"
	trialArmsConstraint    = fmt.Sprintf("JSON 配列（空は []）。最大 %d 件・id は重複不可・prompt_version はこのビルドに登録済み。codex_chatgpt のアームは jev.trial.codex.enabled がオンのときだけ保存できる（endpoint・provider_only・USD の上限・定価は使えない）。provider_only と data_collection は openrouter 専用。endpoint は省略（/v1/systemone）・/alpha/decisions・/v1/chat/completions のどれかで、/v1/chat/completions（一般チャットモデル。/api/v1/… や任意の URL は不可）のアームは list_price_usd_per_mtok（入力）と list_price_output_usd_per_mtok（出力）を両方 0 より大きく指定する必要があり、出力の定価は chat の endpoint だけで使える", jevtrialsettings.MaxArms)
	trialCapConstraint     = fmt.Sprintf("0 より大きく %g 以下の数（USD）", jevshadowsettings.MaxDailyCostCapUSD)
)

var specs = []Spec{
	{papersettings.KeyInitialCapitalJPY, UnitYen, "Paper の運用資金（円）。1 銘柄・総エクスポージャ・1 回の最大損失の % はこの金額が基準", "0 円より大きく 1 兆円（1000000000000）以下", false},
	{papersettings.KeyMaxPositionPerSymbolPct, UnitPercent, "Paper の 1 銘柄あたりの建玉上限（運用資金に対する %）", "0 より大きく 100 以下", false},
	{papersettings.KeyMaxTotalExposurePct, UnitPercent, "Paper の総エクスポージャ上限（運用資金に対する %）", "0 より大きく 100 以下", false},
	{papersettings.KeyMaxTradeLossPct, UnitPercent, "Paper の 1 回の最大損失（運用資金に対する %）", fmt.Sprintf("%v 以上 %v 以下", papersettings.MinTradeLossPct, papersettings.MaxTradeLossPct), false},
	{paperpolicysettings.KeyLongMinEntryQuality, UnitText, "Paper のロングの最低エントリー品質（Jev の entry_quality がこれ未満なら見送る）。Paper でだけ効く（Live の policy.long.min_entry_quality は strong のままで、この API からは変えられない）。既定 good", entryQualityConstraint, false},
	{paperpolicysettings.KeyShortMinEntryQuality, UnitText, "Paper のショートの最低エントリー品質（Jev の entry_quality がこれ未満なら見送る）。Paper でだけ効く（Live の policy.short.min_entry_quality は strong のままで、この API からは変えられない）。既定 good", entryQualityConstraint, false},
	{bucketsettings.KeyStockBudgetJPY, UnitYen, "Paper の株の予算枠（円）。保有中の株の建玉（時価ベース）に今回の額を足してこの額を超える新規エントリーは見送る。0 は株の新規エントリーを止める", bucketConstraint, false},
	{bucketsettings.KeyBondETFBudgetJPY, UnitYen, "Paper の債券ETFの予算枠（円）。0 はこの枠を使わない", bucketConstraint, false},
	{dailyguardsettings.KeyOperatingCapitalJPY, UnitYen, "日次ガードの運用資金（円）。日次損失上限・利益確保の発動額を割合から出す基準。0 は未設定", "0 以上 1 兆円以下", false},
	{dailyguardsettings.KeyLossLimitPct, UnitPercent, "日次損失上限（運用資金に対する %）。到達で新規エントリーを止める。0 は使わない", "0 以上 100 以下", false},
	{dailyguardsettings.KeyProfitLockTriggerPct, UnitPercent, "利益確保の発動水準（運用資金に対する %）。0 は使わない", "0 以上 100 以下", false},
	{dailyguardsettings.KeyProfitLockGivebackPct, UnitPercent, "利益確保の戻し許容（その日の最高損益から何 % 戻したら止めるか）。0 は使わない", "0 以上 100 以下", false},
	{dailyguardsettings.KeyMaxTrades, UnitCount, "1 日の最大エントリー回数。0 は使わない", "0 以上の整数", false},
	{dailyguardsettings.KeyLossLimitJPY, UnitYen, "日次損失上限（円で直接指定。運用資金と % を使わない場合）。0 は使わない", "0 以上", false},
	{dailyguardsettings.KeyProfitLockTriggerJPY, UnitYen, "利益確保の発動水準（円で直接指定）。0 は使わない", "0 以上", false},
	{metalabelsettings.KeyLLMCutoff, UnitDate, "Jev が使う LLM の学習データの終端日（UTC）。OOS 評価はこの日以降の判断だけを使う。ベンダー公式のモデルカード／ドキュメントの knowledge cutoff の日付を入れる（公式に無ければ推測せず仮置きのまま）。仮置き（2026-01-01）のままだとメタモデルは採用されない。日付だけでは unverified、下のモデル名まで記録すると confirmed（settings_get の llm_cutoff_status）", "YYYY-MM-DD", false},
	{metalabelsettings.KeyLLMCutoffModel, UnitText, "上の終端日を確認した Jev のモデル名（出典）。判断の記録の model_id と同じ表記で入れる。実際に応答しているモデルと違うと再確認まで不採用", fmt.Sprintf("%d 文字以内・制御文字なし", metalabelsettings.MaxProvenanceLen), false},
	{metalabelsettings.KeyLLMCutoffConfirmedOn, UnitDate, "上の終端日を公式情報で確認した日（出典。記録用）", "YYYY-MM-DD", false},
	{metalabelsettings.KeyLLMCutoffSource, UnitText, "上の終端日の根拠（出典。確認した公式ページの URL か文書名）", fmt.Sprintf("%d 文字以内・制御文字なし", metalabelsettings.MaxProvenanceLen), false},
	{jevshadowsettings.KeyEnabled, UnitText, "Jev のお試し判断（シャドー）のオン・オフ。オンでも取引には使わず、結果は別表に残すだけ。既定 false（オフ）。1 日の費用上限・失敗回数の上限・呼び出し間隔・429/529 での停止はこの API から変えられない", shadowEnabledConstraint, false},
	{jevshadowsettings.KeyCandidateVersion, UnitText, "シャドーが動かす候補の Trader の版。ライブの版の切り替え（昇格）ではない。既定は未設定（候補なし）。未設定のままオンにはできない", shadowCandidateConstraint, false},
	{researchlabelsettings.KeyEnabled, UnitText, "研究用ホライズンラベル（30 分・60 分・引けまでのコスト前後の bps）の日次記録のオン・オフ。記録は研究専用の表に書くだけで取引には使わない。既定 0（オフ）で、オフの間は日次タスクが何も書かない。保持期間（research.labels.retention_days）はこの API から変えられない", researchLabelEnabledConstraint, false},
	{researchfxsettings.KeyEnabled, UnitText, "研究用の為替 1 分足（USD_JPY など）の記録のオン・オフ。GMOコイン 外国為替FX の公開 API から 1 分足を取り、研究専用の表に書くだけで取引には使わない。既定 0（オフ）で、オフの間は外部への要求も書き込みもない。通貨ペア（research.fx.symbols）はこの API から変えられない", researchLabelEnabledConstraint, false},
	{researchindexsettings.KeyEnabled, UnitText, "研究用の指数（日経平均・TOPIX など）の 1 分値の記録のオン・オフ。立花の日中の時価の記録（#1083）に残っている指数の値を、研究専用の表へ写すだけで、どこにも要求を出さず、取引には使わない。既定 0（オフ）で、オフの間は読み書きもない。指数コード（research.index.symbols）はこの API から変えられない", researchLabelEnabledConstraint, false},
	{tachibanasource.KeyResearchBarsEnabled, UnitText, "立花の日足を夜間に daily_bars へ記録する研究用収集（ブローカーが kabu のままでも動く）のオン・オフ。記録だけで取引には使わない。既定 0（オフ）で、オフまたは立花の認証情報が未設定の間は立花へ何も要求しない。取得速度・時刻・市場区分は変えられない", researchSwitchConstraint, false},
	{tachibanasource.KeyResearchQuotesEnabled, UnitText, "立花の日中の時価を kabu と並べて研究用の別テーブルに記録するもの（最大 120 銘柄）のオン・オフ。記録だけで取引には使わない。既定 0（オフ）で、オフまたは認証情報が未設定の間は立花へ接続も要求もしない。銘柄・接続回数・REST 間隔・保持期間はこの API から変えられない", researchSwitchConstraint, false},
	{researchjobsettings.KeyEvalEnabled, UnitText, "Jev の評価の主な数字（日ごと・モデルと版ごとの和と件数）を毎日の引け後に研究専用の表へ 1 行ずつ残す日次記録のオン・オフ。取引には使わない。既定 0（オフ）で、オフの間は何も書かない。保持期間（research.eval.retention_days）はこの API から変えられない", researchLabelEnabledConstraint, false},
	{researchjobsettings.KeyModelEnabled, UnitText, "特徴量だけで学習する研究用モデル（AI を使わない）の夜間の学習と予測の記録のオン・オフ。予測は研究専用の表に残すだけで取引には使わない。既定 0（オフ）で、オフの間は学習が動かず何も書かない。保持期間（research.model.retention_days）はこの API から変えられない", researchLabelEnabledConstraint, false},
	{researchjobsettings.KeyExportEnabled, UnitText, "training_dataset_export（学習用データセットの書き出し。ローカルのフォルダへ書くだけで外へは送らない）のオン・オフ。既定 0（オフ）で、オフの間はツールが何も書き出さない", researchLabelEnabledConstraint, false},
	{jevtrialsettings.KeyEnabled, UnitText, "お試し記録（複数モデルの比較。ライブの判断と同じ状態を各アームのモデルにも聞いて別表に残すだけで、取引には使わない。Paper のみ）のオン・オフ。既定 false（オフ）。アームが空のままオンにしても何も呼ばない。オンにすると jev.trial.arms のアームごとに有料の呼び出しが発生しうる（全体は jev.trial.daily_cost_cap_usd、アームごとは各アームの上限、Codex は回数とトークンの上限で止まる）。失敗回数の上限・呼び出し間隔・429/529 の待機・停止の記録はこの API から変えられない", trialEnabledConstraint, false},
	{jevtrialsettings.KeyArms, UnitText, "お試し記録のアーム（比べるモデル）の一覧。JSON 配列を丸ごと書き込む（追加・編集・削除・複製はどれも新しい配列全体を送る）。各要素は id・provider（openrouter / custom / codex_chatgpt）・model・prompt_version など。既定は [] （アームなし）。送る前に settings_get の value を取り、変更して返す。Settings 画面と同じ検証をして、通らなければ保存しない", trialArmsConstraint, false},
	{jevtrialsettings.KeyDailyCostCapUSD, UnitText, "お試し記録の全アーム合計の 1 日の費用上限（USD。JST の日ごと）。到達でその日のお試し全体が止まる。既定 0.1。上限を外す（なしにする）ことはできない", trialCapConstraint, false},
	{jevtrialsettings.KeyCodexEnabled, UnitText, "お試し記録の ChatGPT サブスク（Codex CLI のログイン。provider が codex_chatgpt のアーム）のオン・オフ。既定 false（オフ）で、オフの間は codex_chatgpt のアームは呼ばれず保存もできない。サブスクは USD ではなく呼び出し回数とトークン数で数える", trialSwitchConstraint, false},
	{jevtrialsettings.KeyCodexDailyCallCap, UnitCount, "ChatGPT サブスクのお試し呼び出しの 1 日（JST）の回数上限。既定 30。到達でその日のサブスクのアームが止まる", fmt.Sprintf("1 以上 %d 以下の整数", jevtrialsettings.MaxCodexDailyCallCap), false},
	{jevtrialsettings.KeyCodexDailyTokenCap, UnitCount, "ChatGPT サブスクのお試し呼び出しの 1 日（JST）のトークン数（入力＋出力）上限。既定 500000。到達でその日のサブスクのアームが止まる", fmt.Sprintf("1 以上 %d 以下の整数", jevtrialsettings.MaxCodexDailyTokenCap), false},
}

// Specs returns the whitelist in display order. The slice is a copy.
func Specs() []Spec { return slices.Clone(specs) }

// Keys returns the whitelisted keys in display order.
func Keys() []string {
	keys := make([]string, len(specs))
	for i, s := range specs {
		keys[i] = s.Key
	}
	return keys
}

// Lookup returns key's Spec; ok is false for any key not on the whitelist.
func Lookup(key string) (Spec, bool) {
	for _, s := range specs {
		if s.Key == key {
			return s, true
		}
	}
	return Spec{}, false
}

// IsAllowed reports whether key may be written from the MCP.
func IsAllowed(key string) bool {
	_, ok := Lookup(key)
	return ok
}

// secretName matches key names that look secret-class. A whitelisted key that
// matches would be masked even without Spec.Secret, and a test rejects adding
// one to the whitelist unmarked.
var secretName = regexp.MustCompile(`(?i)(secret|password|passwd|token|api_?key|private_?key|credential)`)

// notSecretNames are whitelisted keys whose name matches secretName without
// holding a credential: the Codex trial's daily cap on LLM tokens (a count).
var notSecretNames = []string{jevtrialsettings.KeyCodexDailyTokenCap}

// IsSecret reports whether key's values must be masked: the key is marked
// secret or its name looks secret-class. A key that is not on the whitelist
// is treated as secret (masked by default).
func IsSecret(key string) bool {
	s, ok := Lookup(key)
	return !ok || s.Secret || (secretName.MatchString(key) && !slices.Contains(notSecretNames, key))
}

// Mask returns value, or Masked when key is secret-class (IsSecret).
func Mask(key, value string) string {
	if IsSecret(key) {
		return Masked
	}
	return value
}

// Text renders a write switch state as stored in the audit log: "1" or "0".
func Text(enabled bool) string {
	if enabled {
		return "1"
	}
	return "0"
}

// Apply decodes jsonValue - a runtime_settings.value for KeyWriteEnabled, the
// JSON integer 0 or 1.
func Apply(jsonValue string) (enabled bool, err error) {
	return applySwitch(KeyWriteEnabled, jsonValue)
}

// ApplyOrderProposals decodes jsonValue - a runtime_settings.value for
// KeyOrderProposalsEnabled, the JSON integer 0 or 1.
func ApplyOrderProposals(jsonValue string) (enabled bool, err error) {
	return applySwitch(KeyOrderProposalsEnabled, jsonValue)
}

func applySwitch(key, jsonValue string) (bool, error) {
	var n *float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return false, fmt.Errorf("mcpwritesettings: decode runtime setting %s=%q as a number: %w", key, jsonValue, err)
	}
	if n == nil || (*n != 0 && *n != 1) {
		return false, fmt.Errorf("mcpwritesettings: runtime setting %s=%q must be 0 or 1", key, jsonValue)
	}
	return *n == 1, nil
}
