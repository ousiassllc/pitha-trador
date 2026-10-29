package settings

import (
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/logsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/orderflowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/paperpolicysettings"
	"github.com/ousiassllc/pitha-trador/internal/config/snapshotagesettings"
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

// opsGroup is one card/modal of the Settings screen's 運用設定 section.
// note says when a saved value takes effect (restart needed or not).
type opsGroup struct {
	id          string
	name        string
	description string
	note        string
	fields      []opsField
}

const (
	thresholdNote = "設定ファイルの既定値より、ここで保存した値が優先されます。"
	hotApplyNote  = "保存した値は再起動不要で、次の評価（次の候補更新）から反映されます。"
)

// opsGroups is the 運用設定 section in display order. Together the groups
// must cover opsettings.Keys() exactly once (settings_ops_groups_test.go).
var opsGroups = slices.Concat([]opsGroup{
	brokerGroup,
	tachibanaGroup,
}, tachibanaSourceGroups, []opsGroup{
	paperRiskGroup,
	bucketGroup,
	dailyGuardGroup,
	costGroup,
	metaLabelGroup,
	regimeGroup,
	paperBootstrapGroup,
	sleepGuardGroup,
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
	{
		id:          "logsettings",
		name:        "ログのレベルと保持期間",
		description: "日次 JSON ログに書き出す最低レベルと、ログファイルを圧縮せずに置いておく日数です。INFO 以上には計測ログ（メトリクスなど）が含まれ、期間指定のエクスポートの元になります。",
		note: "ログレベルは起動時に一度だけ読むため、保存・既定に戻すの反映にはアプリの再起動が必要です。error にするとエクスポートできる計測ログ（INFO）や WARN が残りません。" +
			"保持期間は再起動不要で、次のメンテナンスチェック（10 分以内）から反映されます。環境変数では設定できません。",
		fields: []opsField{
			{key: config.KeyLogLevel, label: "ログレベル", options: logsettings.Levels,
				hint: "debug / info / warn / error のいずれかです。既定は info です。debug はデータベースの正常クエリも記録するため、ログが大きくなります。"},
			{key: config.KeyLogRetentionDays, label: "ログの保持期間（日）", placeholder: "30",
				hint: "1〜3650 の整数です。この日数より古い日次ログは gzip で圧縮アーカイブされます。既定は 30 日です。"},
		},
	},
	policyGroup("policy-long", "Policy Engine（ロング）", "policy.long.", "ロング"),
	policyGroup("policy-short", "Policy Engine（ショート）", "policy.short.", "ショート"),
	{
		id:          "screener",
		name:        "Fast Screener",
		description: "Fast Screener の絞り込みしきい値・上位件数・スコア重みです。",
		note: thresholdNote + hotApplyNote +
			"Policy Engine のスプレッド・板厚さの判定は、ここではなく設定ファイルのスプレッド上限・直近 5 分売買代金の下限を使い、ここの値の影響を受けません。",
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
	{
		id:          "snapshot-age",
		name:        "データの鮮度（ランキング監視）",
		description: "ランキング監視モード（既定）で、Jev Scout / Trader と Paper エントリーが最新の足をどこまで古くてよしとするかの上限（秒）です。これより古い足では判断に進まず、そのジョブは見送られます。",
		note: "立会時間中のみ適用されます。全銘柄 REST モードでは、この値ではなく設定ファイルの別の鮮度上限が使われます。" +
			"価格は kabu の PUSH を優先し、PUSH が古い・未着のときだけ REST に切り替えます。Jev に渡す state と判断ログには、判断時点でのデータの古さが含まれます。" +
			"保存した値の反映にはアプリの再起動が必要です。環境変数では設定できません。",
		fields: []opsField{
			{key: snapshotagesettings.Key, label: "足の鮮度上限（秒）", hint: "1〜600 の整数です。既定は 20 秒です。大きくするほど古いデータで判断することになります。"},
		},
	},
	{
		id:          "orderflow",
		name:        "板の需給（OFI）",
		description: "kabu の PUSH 最良気配の数量変化から求める Order Flow Imbalance を集計するローリング窓の幅（秒）です。マイクロプライス乖離とあわせて特徴量として保存します。",
		note: "保存した値は再起動不要で、次の市場データ取得（次の 1 分足）から反映されます。" +
			"OFI は特徴量・フィルタ用途に限り、OFI 単独で発注することはありません。立花証券を選択している間は PUSH 最良気配を取得しないため、OFI は算出されません。",
		fields: []opsField{
			{key: orderflowsettings.KeyShort, label: "短期窓（秒）", hint: "1 以上の整数です。短期 < 中期 < 長期（最大 600 秒）にしてください。既定は 10 秒です。"},
			{key: orderflowsettings.KeyMid, label: "中期窓（秒）", hint: "短期窓より大きくしてください。既定は 30 秒です。"},
			{key: orderflowsettings.KeyLong, label: "長期窓（秒）", hint: "中期窓より大きく、600 秒以下にしてください。既定は 60 秒です。"},
		},
	},
	{
		id:          "triple_barrier",
		name:        "キャリブレーションのラベル（トリプルバリア）",
		description: "Jev の判断に正解・不正解のラベルを付けるときの利確（TP）・損切り（SL）の幅、時間切れ（timeout）、売買コストの想定です。先に触れたバリアでラベルを決め、コスト控除後のリターンで評価します。",
		note: "TP・SL はコストを差し引いた後のリターン（bps、1 bps = 0.01%）の幅です。TP は価格が有利に TP+コスト、SL は不利に SL−コスト動いたときに触れます。" +
			"保存した値は再起動不要で、以降に付くラベルから反映されます（付与済みのラベルは変わりません）。環境変数では設定できません。",
		fields: []opsField{
			{key: barriersettings.KeyTakeProfitBps, label: "利確幅 TP（bps）", hint: "0 より大きく 1000 以下です。既定は 30 bps です。"},
			{key: barriersettings.KeyStopLossBps, label: "損切り幅 SL（bps）", hint: "0 より大きく 1000 以下で、コスト想定より大きくしてください。既定は 30 bps です。"},
			{key: barriersettings.KeyTimeoutMinutes, label: "時間切れ（分）", hint: "1〜60 の整数です。この時間内にどちらのバリアにも触れなければ、その時点のコスト控除後リターンで評価します。既定は 10 分です。"},
			{key: barriersettings.KeyCostBps, label: "コスト想定（bps）", hint: "手数料＋スプレッド＋スリッページの往復合計です。0 以上 1000 以下で、損切り幅より小さくしてください。既定は 10 bps です。"},
		},
	},
})

// policyGroup builds the LONG or SHORT Policy Engine group; prefix is
// "policy.long." or "policy.short.".
func policyGroup(id, name, prefix, direction string) opsGroup {
	return opsGroup{
		id:          id,
		name:        name,
		description: "Jev Trader の判断が" + direction + "シグナルになるためのしきい値です。",
		note: thresholdNote + hotApplyNote +
			"自己改善ループ（Sol / Opus）も同じキーを更新するため、後から書き込んだ方の値が有効です。",
		fields: []opsField{
			{key: prefix + "min_probability", label: "最低確率", hint: "0 より大きく 1 以下です。"},
			{key: prefix + "min_entry_quality", label: "最低エントリー品質（Live）", options: entryQualityOptions, hint: "Live で使う値です（既定 strong。MCP からは変えられません）。Paper では使われず、Paper の値が使われます。"},
			{key: paperpolicysettings.PaperKeyOf(prefix + "min_entry_quality"), label: "最低エントリー品質（Paper）", options: entryQualityOptions, hint: "Paper でだけ使う値です（既定 good。MCP からも変えられます）。他のしきい値は Live と共通です。"},
			{key: prefix + "min_continuation_probability", label: "継続確率の下限", hint: "0 より大きく 1 以下です。"},
			{key: prefix + "max_toxic_flow", label: "有害フロー上限", hint: "0 より大きく 1 以下です。"},
			{key: prefix + "max_liquidity_stressed", label: "流動性ストレス上限", hint: "0 より大きく 1 以下です。"},
		},
	}
}

// jevShadowFieldLabels are the names of the two MCP-changeable settings that are
// not operational settings (opsGroups) but the Jev prompt panel's own fields
// (issue #929): the labels that panel gives them.
var jevShadowFieldLabels = map[string]string{
	jevshadowsettings.KeyEnabled:          "シャドーを有効にする",
	jevshadowsettings.KeyCandidateVersion: "シャドーの候補の版",
}

// FieldLabel is the name the Settings screen gives the operational setting
// key, for the screens that mention a setting (the experiments of the MCP
// card) without showing its key. A label that more than one setting has (the
// LONG and SHORT Paper entry quality) is led by its group's name, so the two
// stay apart. A key no group lists is not printed.
func FieldLabel(key string) string {
	if label, ok := jevShadowFieldLabels[key]; ok {
		return label
	}
	group, ok := opsGroupByKey(key)
	if !ok {
		return "その他の設定"
	}
	label := ""
	for _, field := range group.fields {
		if field.key == key {
			label = field.label
		}
	}
	if fieldLabelCount(label) > 1 {
		return group.name + "の" + label
	}
	return label
}

func fieldLabelCount(label string) int {
	n := 0
	for _, group := range opsGroups {
		for _, field := range group.fields {
			if field.label == label {
				n++
			}
		}
	}
	return n
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
