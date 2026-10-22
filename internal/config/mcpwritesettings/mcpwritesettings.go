// Package mcpwritesettings defines what the Paper results MCP may change
// (issues #806, #807): the whitelist of runtime_settings keys its settings
// tools can write, the switch that turns those writes on and off, and the
// masking rule for audit and tool output.
//
// The whitelist is the only way a key becomes writable from the MCP; a key not
// listed here is rejected before anything is read or stored. It deliberately
// holds only Paper and operating-capital values: the Paper capital and
// exposure limits (papersettings), the per-asset-class budgets
// (bucketsettings), the daily entry-halt thresholds
// (dailyguardsettings), the Paper-only minimum entry quality
// (paperpolicysettings, issue #861) and the LLM cutoff with its provenance
// (metalabelsettings). It never holds a Live value (policy.long|short.
// min_entry_quality, the Live thresholds, are not on it), the Paper bootstrap
// switch, a secret, or anything that orders, moves money or releases the Kill
// Switch - those have no key in runtime_settings the MCP could reach through
// this list.
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
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/paperpolicysettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
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

// IsSecret reports whether key's values must be masked: the key is marked
// secret or its name looks secret-class. A key that is not on the whitelist
// is treated as secret (masked by default).
func IsSecret(key string) bool {
	s, ok := Lookup(key)
	return !ok || s.Secret || secretName.MatchString(key)
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
