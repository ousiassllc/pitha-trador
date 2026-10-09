package settings

import (
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// entryQualityOptions are the values of a policy.*.min_entry_quality field,
// worst to best.
var entryQualityOptions = []string{
	domain.JevEntryQualityPoor, domain.JevEntryQualityFair, domain.JevEntryQualityGood,
	domain.JevEntryQualityStrong, domain.JevEntryQualityExceptional,
}

// opsField is one operator-editable, non-secret Settings row: the
// runtime_settings key, its label and optional guidance.
type opsField struct {
	key         string
	label       string
	hint        string
	placeholder string
	options     []string
}

// opsGroup is one card/modal of the Settings screen's 運用設定 section
// (issue #708). note says when a saved value takes effect (restart needed
// or not).
type opsGroup struct {
	id          string
	name        string
	description string
	note        string
	fields      []opsField
}

const (
	thresholdNote = "優先順位は config/strategy.yaml の値 < ここで保存した値です。"
	hotApplyNote  = "保存した値は再起動不要で、次の評価（次の候補更新）から反映されます。"
)

// opsGroups is the 運用設定 section in display order. Together the groups
// must cover opsettings.Keys() exactly once (settings_ops_groups_test.go).
// They replace the removed PITHA_BACKUP_DIR / PITHA_LOG_DIR /
// PITHA_POLICY_* / PITHA_FAST_SCREENER_* environment variables.
var opsGroups = slices.Concat([]opsGroup{
	brokerGroup,
	tachibanaGroup,
}, tachibanaSourceGroups, []opsGroup{
	{
		id:          "backup",
		name:        "バックアップ先",
		description: "SQLite データベースの日次バックアップ（直近 90 日分）と週次アーカイブ（52 週分）の保存先ディレクトリです。",
		note: "未設定の間は日次バックアップは無効です。保存後は再起動不要で、10 分以内の次回メンテナンスチェックからバックアップが動きます（空にして「既定に戻す」で再び無効にできます）。" +
			"指定先が存在しない場合（外付けドライブ未接続など）はローカルへ退避せずエラーとなり、連続失敗時に通知されます。",
		fields: []opsField{{
			key:         config.KeyBackupDir,
			label:       "バックアップ先ディレクトリ",
			hint:        "このアプリのディスクの外にある既存ディレクトリ（外付けドライブ・クラウド同期フォルダなど）を絶対パスで入力してください。シークレット（API キー等）はバックアップから除外されます。",
			placeholder: "例: /mnt/backup/pitha-trador",
		}},
	},
	{
		id:          "logdir",
		name:        "ログディレクトリ",
		description: "日次 JSON ログとエラーログのダウンロード元になるディレクトリです（任意）。",
		note:        "未設定なら DB と同じ階層の logs/ を使います。ログは起動時に開くため、保存・削除の反映にはアプリの再起動が必要です。",
		fields: []opsField{{
			key:         config.KeyLogDir,
			label:       "ログディレクトリ",
			hint:        "絶対パスで入力してください。ディレクトリが作成できない場合、アプリは標準エラー出力へログを出して起動を続けます。",
			placeholder: "未設定（既定を使用）",
		}},
	},
	policyGroup("policy-long", "Policy Engine（ロング）", "policy.long.", "ロング"),
	policyGroup("policy-short", "Policy Engine（ショート）", "policy.short.", "ショート"),
	{
		id:          "screener",
		name:        "Fast Screener",
		description: "Fast Screener の絞り込みしきい値・上位件数・スコア重みです（FR-FS-1 / FR-FS-3）。",
		note: thresholdNote + hotApplyNote +
			"Policy Engine のスプレッド・板厚さ判定（FR-POLICY-3）は strategy.yaml の max_spread_bps / min_turnover_5m_jpy を使い、ここの値の影響を受けません。",
		fields: []opsField{
			{key: "screener.min_price", label: "最低株価（円）"},
			{key: "screener.max_price", label: "最高株価（円）", hint: "最低株価以上にしてください。"},
			{key: "screener.min_turnover_5m_jpy", label: "直近 5 分売買代金の下限（円）"},
			{key: "screener.max_spread_bps", label: "スプレッド上限（bps）"},
			{key: "screener.min_volume_ratio", label: "出来高倍率の下限"},
			{key: "screener.min_abs_return_5m_pct", label: "5 分騰落率（絶対値）の下限（%）"},
			{key: "screener.min_realized_volatility", label: "実現ボラティリティの下限"},
			{key: "screener.top_n", label: "候補の上位件数", hint: "1 以上の整数です。"},
			{key: "screener.weights.volume_ratio", label: "重み: 出来高倍率", hint: "重みは 0 以上で、少なくとも 1 つは 0 より大きくしてください。"},
			{key: "screener.weights.abs_return_5m", label: "重み: 5 分騰落率"},
			{key: "screener.weights.breakout_strength", label: "重み: ブレイクアウト強度"},
			{key: "screener.weights.orderbook_imbalance", label: "重み: 板の偏り"},
			{key: "screener.weights.volatility_expansion", label: "重み: ボラティリティ拡大"},
		},
	},
})

// policyGroup builds the LONG or SHORT Policy Engine group; prefix is
// "policy.long." or "policy.short.".
func policyGroup(id, name, prefix, direction string) opsGroup {
	return opsGroup{
		id:          id,
		name:        name,
		description: "Jev Trader の判断が" + direction + "シグナルになるためのしきい値です（FR-POLICY-1 / FR-POLICY-2 / FR-POLICY-4）。",
		note: thresholdNote + hotApplyNote +
			"自己改善ループ（Sol / Opus）も同じキーを更新するため、後から書き込んだ方の値が有効です。",
		fields: []opsField{
			{key: prefix + "min_probability", label: "最低確率（min_probability）", hint: "0 より大きく 1 以下です。"},
			{key: prefix + "min_entry_quality", label: "最低エントリー品質（min_entry_quality）", options: entryQualityOptions},
			{key: prefix + "min_continuation_probability", label: "継続確率の下限（min_continuation_probability）", hint: "0 より大きく 1 以下です。"},
			{key: prefix + "max_toxic_flow", label: "有害フロー上限（max_toxic_flow）", hint: "0 より大きく 1 以下です。"},
			{key: prefix + "max_liquidity_stressed", label: "流動性ストレス上限（max_liquidity_stressed）", hint: "0 より大きく 1 以下です。"},
		},
	}
}

func opsGroupByKey(key string) (opsGroup, bool) {
	for _, group := range opsGroups {
		for _, field := range group.fields {
			if field.key == key {
				return group, true
			}
		}
	}
	return opsGroup{}, false
}
