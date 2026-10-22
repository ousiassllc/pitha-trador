# API仕様: API ルート（§5 続き: 実績・Calibration・自己改善・アクティビティ）

`docs/api/endpoints/huma-api.md`（§5）から分割した節（300行/ファイル制限のため）。節番号・内容は変更なし。Humaの共通方針（OpenAPIスペック・エラー形式・ページング等）、スキャナ・銘柄・シグナル・ポジション・注文・システム状態・エラーログの各ルートとエンドポイント一覧表は `docs/api/endpoints/huma-api.md` を参照。

### GET /api/v1/performance

全クローズ済みポジション（`positions.closed_at`あり）の実績集計。`total_pnl`/`daily_pnl`は`realized_pnl`の合計（`daily_pnl`はJST当日0時以降にクローズしたもの）。`win_rate`/`expectancy`/`max_drawdown_pct`はバックテスト（`internal/service/backtest.Aggregate`）と同じ定義で、各ポジションのエントリー約定額に対する損益率（%）から算出する。`profit_factor`は総利益÷総損失（損失なしは`null`）、`sharpe_ref`/`sortino_ref`はトレードごとリターンの平均÷標準偏差／下方偏差（年率換算なし、算出不能時は`null`）、`signal_count`はLONG/SHORTの`trade_signals`件数。

```json
// Output（抜粋）
{
  "total_pnl": 128340,
  "daily_pnl": 15200,
  "win_rate": 0.57,
  "profit_factor": 1.82,
  "expectancy": 0.34,
  "max_drawdown_pct": 4.1,
  "average_hold_time_minutes": 14.2,
  "sharpe_ref": 1.1,
  "sortino_ref": 1.6,
  "trade_count": 12,
  "signal_count": 342
}
```

### GET /api/v1/performance/evaluation

実績評価（issue #777、`requirements/functional/performance-evaluation.md`）。クローズ済みPaper建玉を読み取り専用で集計し、サンプル数・95%信頼区間・合格ライン判定つきで返す（書き込み・市場データの生値の出力なし）。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `from` | string | `closed_at`の範囲の最初のJST日（`YYYY-MM-DD`）。省略は最初から |
| `to` | string | 最後のJST日（`YYYY-MM-DD`、その日を含む）。省略は現在まで。形式不正と`to`が`from`より前は422 |

`overall`と、`by_day`/`by_week`/`by_direction`/`by_exit_reason`/`by_meta_model`/`by_ab_arm`の各要素（`key`+同じ指標）は、`n`・`days`・`wins`、円の`net_pnl_jpy`・`fees_jpy`・`slippage_jpy`（約定価格に含まれる分。`slippage_recorded_fills`は記録のある約定数）・`gross_before_fees_jpy`・`gross_before_costs_jpy`、および指標`win_rate`/`ev_jpy`/`ev_bps`/`profit_factor`/`max_drawdown_jpy`/`max_drawdown_pct`（資金比%）を持つ。各指標は`{n, value, lo, hi, has_ci, defined, reliable}`で、`reliable: false`は「判定不能」（サンプルまたは取引日数が基準未満、定義不能、区間なし。`value`は参考値）。`initial_capital`は`risk.yaml`の値（Paperでは仮置き。0以下は未設定で資金比は判定不能）、`criteria`は適用した基準値。`jev[]`は`label_version`別の`hit_rate`・`brier`、`meta[]`はメタモデルごとにOOS（`oos_auc`・`oos_expected_bps`・`oos_samples`・`oos_reliable`）と、採用取引の`linked`・`predicted_ev_bps`・`ev_gap_bps`（実現−予測）、OOS期間内の全取引`in_oos_window`を並べる。`verdict`は`{status: pass|fail|undetermined, checks[]}`で、`checks[].key`は`period_days`/`trades`/`ev_lower_bound`/`max_drawdown`（`required`・`actual`・`status`）。Live前チェックの材料であり、判定がLive切替・取引・設定に接続されることはない。

```json
// Output（抜粋）
{
  "initial_capital": 30000000,
  "criteria": { "min_sample": 30, "min_clusters": 5, "min_days": 20, "min_trades": 100, "min_ev_lower_bps": 0, "max_drawdown_pct": 5 },
  "overall": {
    "n": 6, "days": 3, "wins": 3, "net_pnl_jpy": -1000, "fees_jpy": 100,
    "win_rate": { "n": 6, "value": 0.5, "lo": 0.1, "hi": 0.9, "has_ci": true, "defined": true, "reliable": false },
    "max_drawdown_jpy": { "n": 6, "value": 1760, "lo": 0, "hi": 3000, "has_ci": true, "defined": true, "reliable": false },
    "max_drawdown_pct": { "n": 6, "value": 0.0059, "lo": 0, "hi": 0.01, "has_ci": true, "defined": true, "reliable": false }
  },
  "verdict": { "status": "undetermined", "checks": [ { "key": "trades", "required": 100, "actual": 6, "has_actual": true, "status": "undetermined" } ] }
}
```

### GET /api/v1/calibration

| クエリ | 型 | 説明 |
|-------|-----|------|
| `horizon` | string | 集計する判定水平線（分）: `5` / `10` / `15` / `all`（既定`all`）。`all`は現行水平線`5`/`10`/`15`のラベルと、トリプルバリアのラベル（horizonはSettingsのtimeout。FR-CAL-4）をまとめて集計する。範囲外（`20`・数値以外・`0`等）は422 |

```json
// Output（抜粋）
{
  "horizon": "all",
  "buckets": [
    { "range": "0.50-0.60", "avg_confidence": 0.55, "direction_accuracy": 0.51, "avg_future_return_pct": -0.05,
      "sample_count": 80, "trade_count": 6, "total_pnl": -1800, "avg_pnl_pct": -0.21, "trade_win_rate": 0.5,
      "triple_barrier": { "sample_count": 30, "tp_count": 6, "sl_count": 14, "timeout_count": 10, "tp_rate": 0.2, "sl_rate": 0.467, "timeout_rate": 0.333, "ev_net_bps": -9.4 } },
    { "range": "0.60-0.70", "avg_confidence": 0.65, "direction_accuracy": 0.55, "avg_future_return_pct": 0.02,
      "sample_count": 64, "trade_count": 9, "total_pnl": 400, "avg_pnl_pct": 0.03, "trade_win_rate": 0.5 },
    { "range": "0.70-0.80", "avg_confidence": 0.75, "direction_accuracy": 0.63, "avg_future_return_pct": 0.11,
      "sample_count": 41, "trade_count": 12, "total_pnl": 5200, "avg_pnl_pct": 0.18, "trade_win_rate": 0.5 },
    { "range": "0.80-0.90", "avg_confidence": 0.85, "direction_accuracy": 0.71, "avg_future_return_pct": 0.24,
      "sample_count": 22, "trade_count": 7, "total_pnl": 6100, "avg_pnl_pct": 0.31, "trade_win_rate": 0.5 },
    { "range": "0.90-1.00", "avg_confidence": 0.94, "direction_accuracy": 0.78, "avg_future_return_pct": 0.39,
      "sample_count": 9, "trade_count": 3, "total_pnl": 3300, "avg_pnl_pct": 0.42, "trade_win_rate": 0.5 }
  ],
  "by_direction": [
    { "direction": "LONG", "sample_count": 120, "direction_accuracy": 0.62, "avg_future_return_pct": 0.14 },
    { "direction": "SHORT", "sample_count": 96, "direction_accuracy": 0.58, "avg_future_return_pct": 0.09 }
  ],
  "brier_score": 0.19,
  "log_loss": 0.52,
  "expected_calibration_error": 0.06,
  "trade_count": 20,
  "pnl_brier_score": 0.27,
  "pnl_log_loss": 0.74,
  "triple_barrier": { "sample_count": 120, "tp_count": 38, "sl_count": 46, "timeout_count": 36, "tp_rate": 0.317, "sl_rate": 0.383, "timeout_rate": 0.3, "ev_net_bps": 1.8 },
  "ab_arms": [
    { "arm": "momentum", "trade_count": 12, "win_rate": 0.417, "ev_net_bps": -2.1, "total_pnl": -3200,
      "by_regime": [ { "regime": "trend", "trade_count": 8, "win_rate": 0.5, "ev_net_bps": 1.4, "total_pnl": 900 },
                     { "regime": "mean_revert", "trade_count": 4, "win_rate": 0.25, "ev_net_bps": -9.8, "total_pnl": -4100 } ] },
    { "arm": "regime", "trade_count": 11, "win_rate": 0.545, "ev_net_bps": 3.2, "total_pnl": 2600,
      "by_regime": [ { "regime": "trend", "trade_count": 7, "win_rate": 0.571, "ev_net_bps": 1.9, "total_pnl": 700 },
                     { "regime": "mean_revert", "trade_count": 4, "win_rate": 0.5, "ev_net_bps": 5.6, "total_pnl": 1900 } ] }
  ],
  "meta_model": {
    "version": "lr-20261014-210000", "algorithm": "logistic", "status": "rejected", "reject_reason": "oos_auc_ci_not_above_minimum",
    "active_version": "lr-20261013-210000", "trained_at": "2026-10-14T12:00:00Z",
    "train_start": "2026-09-01T00:00:00Z", "train_end": "2026-10-01T00:00:00Z",
    "oos_start": "2026-10-01T00:00:00Z", "oos_end": "2026-10-14T00:00:00Z", "train_samples": 300, "oos_samples": 120,
    "oos_auc": 0.48, "oos_ev_bps": null, "oos_takes": 40, "oos_days": 3, "oos_ev_verdict": "undetermined", "min_detectable_edge_bps": 11,
    "criteria": { "min_oos_samples": 740, "min_takes": 370, "min_oos_days": 7, "min_ev_lower_bps": 0, "min_auc_lower": 0.5, "alpha": 0.01, "resamples": 2000 },
    "oos_calibration": [ { "mean_predicted": 0.55, "observed_rate": 0.5, "count": 40 } ],
    "llm_cutoff": "2026-09-01"
  }
}
```

`by_direction`は予測方向（`LONG`/`SHORT`、常に両方を返す）別の方向別平均リターン（`avg_future_return_pct`は方向調整済み＝SHORTは下落が正）と的中率（FR-CAL-2）。バケットの`trade_count`/`total_pnl`/`avg_pnl_pct`はconfidence bucket別PnL（FR-CAL-2）で、`positions.entry_order_id` → `paper_orders.trade_signal_id` → `trade_signals.jev_decision_id`で辿れるTrader判断由来のクローズ済みポジションの件数・実現損益合計（JPY）・エントリー金額に対する平均リターン（%）。手動エントリーは含まない。指標は`jev_decisions.question_version`で分離せず、全版のTrader判断（`trader-v2`/`trader-v3`等）を混在して集計する（版別・現行版のみの絞り込みやクエリは無い）。

実現トレードPnLをground truthとする評価（FR-CAL-5）: バケットの`trade_win_rate`は`trade_count`のうち`realized_pnl > 0`の割合（`trade_count`が0なら0）、トップレベルの`trade_count`は`pnl_brier_score`/`pnl_log_loss`の対象となったクローズ済みポジション数（confidenceがどのバケットにも入らないものを除く。0件なら両スコアも0）。両スコアはconfidenceを予測確率、`realized_pnl > 0`を結果としたBrier Score/Log Loss。`realized_pnl`は両約定の手数料控除後・約定モデル（FR-ENTRY-8）の呼値/スプレッド/滑りを織り込んだ値。判定水平線の既定は5/10/15分。

トリプルバリア（FR-CAL-4、issue #767）: トップレベルと各バケットの`triple_barrier`は、トリプルバリアのラベル（`barrier_hit`のある`calibration_outcomes`行。旧ラベルは含めない）の`sample_count`（件数）、`tp_count`/`sl_count`/`timeout_count`（先に触れたバリア別の件数）、`tp_rate`/`sl_rate`/`timeout_rate`（各件数÷`sample_count`、0〜1）、`ev_net_bps`（EV（net）＝コスト控除後リターン`net_return_bps`の平均、1判断あたりのbps）を返す。ラベルが0件の帯・全体は全て0（画面は`sample_count`で「ラベルなし」を判定する）。`direction_accuracy`等は従来どおり旧ラベルとトリプルバリアのラベルの両方を`sample_count`に数える。

A/B比較（FR-REGIME-1、issue #755）: `ab_arms`はpaperの固定モメンタム（`momentum`）とレジーム切替（`regime`）の2アームを常にこの順で返す（Settings `regime.ab_enabled`が無効のあいだは全判断が`momentum`アームなので`regime`は0件）。各アームは、そのアームの判断（`jev_decisions.ab_arm`）から建てられクローズ済み（`closed_at`と`realized_pnl`が非NULL）の`positions`の`trade_count`（件数）、`win_rate`（`realized_pnl > 0`の割合、0〜1）、`ev_net_bps`（EV（net）＝`realized_pnl`÷エントリー想定元本の平均、1建玉あたりのbps。`realized_pnl`は両約定の手数料控除後・約定モデル（FR-ENTRY-8）の呼値/スプレッド/滑りを織り込んだ値＝コスト控除後）、`total_pnl`（円）と、そのアームのレジーム別（`by_regime[]`。`regime`は`trend`/`mean_revert`/`unknown`、建玉があるレジームのみ、この順）を返す。建玉が0件のアーム・レジームは件数も勝率・EV・損益も0（画面は`trade_count`で判定する）。判断のdirectionが`NONE`でもVWAP回帰で建てた建玉は数える。レジーム・アームが未評価の判断（旧行）と手動エントリーは含めない。実現PnLに基づくため`horizon`に依存しない。取得元（`calibration.RegimeTradeSource`＝`decisiontrade.Repository`）が未配線なら`ab_arms`キー自体を省く。

メタラベリングのOOS指標（FR-META-5、issue #766）: `meta_model`は最新のメタラベリングモデル（`metalabel_models`の`trained_at`が最新の行。`rejected`も含む）の報告で、`version`/`algorithm`/`status`（`active`=採用、`rejected`=記録のみで`reject_reason`に理由）/`trained_at`/`train_start`〜`train_end`（学習窓）/`train_samples`/`oos_samples`に加え、**Jev評価期間**`oos_start`〜`oos_end`（OOS窓。判断は`llm_cutoff`以降のものだけが対象。`llm_cutoff`は応答時点のSettings `metalabel.llm_cutoff`（UTC日付`YYYY-MM-DD`）、`oos_auc`（0.5=無能力）、`oos_ev_bps`（OOS上でDecideが採ったシグナルの平均純リターン、bps）、`oos_calibration[]`（較正後確率のビンごとの`mean_predicted`・`observed_rate`・`count`）を返す。`oos_auc`/`oos_ev_bps`は定義できないとき`null`。`active_version`は推論で使われているモデル（最新の`active`。無ければ空文字で、その間のエントリーは見送り）で、最新モデルが`rejected`のとき`version`と異なる。モデルが1件も無い、または取得元が未配線のときは`meta_model`キー自体を省く。`horizon`には依存しない。実装は`internal/service/metalabel.Reporter`で、`internal/bootstrap/metalabeljob.CalibrationSource`が`calibration.Service`（`Metrics`）と合成して`router.WithCalibrationSource`へ渡す。ハンドラは取得元が`calibration.MetaModelSource`（`Report`）も実装していれば`meta_model`を返す。

LLM学習データ終了日の状態（FR-META-6、issue #776）: `meta_model_cutoff`はSettings `metalabel.llm_cutoff`の状態で、モデルが未学習でも返る（ソース未配線のときは省略）。`llm_cutoff`（UTC日付`YYYY-MM-DD`）、`status`（`placeholder`=未設定または既定の仮置きで、夜間再学習のモデルは`reject_reason=llm_cutoff_placeholder`で不採用／`unverified`=実日付だが確認したJevモデル名が未記録／`confirmed`）、`warning`（運用者向けの警告。`confirmed`のときは空）、出典の`model`（`metalabel.llm_cutoff_model`）・`confirmed_on`・`source`（未記録は空文字）。`meta_model.reject_reason`は`llm_cutoff_placeholder`（cutoff仮置き）・`llm_model_changed`（OOS窓のJevモデルがcutoff確認時のモデルと異なる）を取り得る。

再学習の見送り（FR-META-4a、issue #799）: `meta_model_skips[]`は夜間のメタモデル再学習がデータ不足で見送られた記録で、新しい順に直近10件。モデルが未学習でも返る（見送りが無い・ソース未配線のときは省略）。各要素は`skipped_at`（UTC）、`reason_code`（固定コード: `too_few_rows_before_oos`=検証窓より前のラベル付き判断がfit+較正の最小件数に足りない／`too_few_rows_after_purge`=purge・embargo後にfitか較正が最小件数に足りない／`training_data_unusable`=件数はあるがラベルが片側だけ等で学習できない。自由文は返さない）、`usable_rows`、`required_rows`（「必要130件中 x件」の130とx）。画面は「必要130件中 x件」と理由名を表に出す。horizonに依存しない。

採用判定の根拠（FR-META-7、issue #780）: `meta_model`は`oos_ev_verdict`（`no_data`＝OOSのtakeが0でデータ無し／`positive`・`negative`＝`oos_ev_ci`が0を片側に外れる／`undetermined`＝判定不能：区間が0をまたぐ（または未定義）かつ`|oos_ev_bps|`が`min_detectable_edge_bps`（約11）未満／`not_significant`＝区間が0をまたぎ11 bps以上。足切りであり安全の証明ではない）・`min_detectable_edge_bps`に加え、`oos_takes`（OOSでtakeした件数）・`oos_days`（OOSが跨ぐ取引日数）・`oos_ev_ci`・`oos_auc_ci`（クラスタ・ブートストラップの信頼区間`{low, high}`。未定義のときは省略）に加え、判定に使った基準`criteria`（`min_oos_samples`/`min_takes`/`min_oos_days`/`min_ev_lower_bps`/`min_auc_lower`/`alpha`/`resamples`）、現行モデルとの同一OOS比較`comparison`（`active_version`/`comparable`/`note`/`paired_samples`/`candidate_bps`/`active_bps`/`diff_bps`/`diff_ci`）、変更頻度制限で止めた場合の`gate`（`reason`/`detail`/`until`）、採用後の前向き評価`forward`（`evaluated_at`/`from`/`samples`/`takes`/`days`/`ev_bps`/`ev_ci`/`degraded`）を返す（#780以前に保存したモデルでは`criteria`以下は省略）。`reject_reason`は`oos_samples_below_minimum`/`oos_days_below_minimum`/`oos_takes_below_minimum`/`oos_ev_ci_not_positive`/`oos_auc_ci_not_above_minimum`/`candidate_not_better_than_active`/`active_model_not_comparable`/`model_swap_rate_limited`/`policy_change_freeze`も取り得る。

`horizon`（issue #719）はビン・方向別・Brier/Log Loss/ECEの全指標を、指定した水平線の`calibration_outcomes`行だけで再計算する（`ListLabeledSamples`が`horizon_minutes`で絞る）。応答の`horizon`は集計した水平線をそのまま返す（画面の表示用）。`all`は`calibration.DefaultHorizonsMinutes`（5/10/15）と全てのトリプルバリアのラベルを合算し（トリプルバリアのhorizonはSettingsのtimeoutで任意の1〜60分のため、5/10/15以外の値は`all`でのみ集計される。`ListCurrentLabeledSamples`）、#711以前の旧20分ラベルは混ざらない（旧ラベルはAPIからも選べない）。バケットの`trade_count`/`total_pnl`/`avg_pnl_pct`/`trade_win_rate`と`pnl_brier_score`/`pnl_log_loss`は実現トレードPnLに基づくためホライズンに依存せず、どの`horizon`でも同じ値を返す。

### GET /api/v1/calibration/exit-alignment

ラベル基準（トリプルバリア）と実出口の整合（FR-CAL-4A、issue #774）。クエリ`days`（1〜365、既定30）は、ラベルと再生を比べる判断の窓（現在から遡る日数）。出力は計算のみで、市場データ由来の値を保存しない（`calibration_outcomes`・`market_snapshots`・`jev_decisions`・`positions`・`metalabel_models`から読み取り時に計算）。取得元（`CalibrationSource`が`AlignmentSource`も実装）が未配線のときは`rows`・`warnings`が空配列、`comparison`・`oos_paper`は省略する。

```json
// Output（抜粋）
{
  "rows": [
    { "key": "take_profit", "item": "利確", "label_text": "TP 30 - ... グロス +40 bps", "exit_text": "+120 bps", "comparable": true, "aligned": false }
  ],
  "warnings": [ { "code": "take_profit_mismatch", "message": "..." } ],
  "tolerance_bps": 5, "tolerance_minutes": 1,
  "comparison": {
    "from": "2026-09-17T00:00:00Z", "to": "2026-10-17T00:00:00Z", "horizon_minutes": 10,
    "replayed_count": 40, "skipped_count": 3,
    "label_net_bps": { "count": 40, "mean": 4.2, "std_dev": 21.0, "min": -30, "p5": -28, "p25": -12, "median": 3, "p75": 20, "p95": 30, "max": 30 },
    "replay_net_bps": { "count": 40, "mean": -1.1, "std_dev": 35.0, "min": -70, "p5": -60, "p25": -20, "median": 0, "p75": 15, "p95": 60, "max": 120 },
    "diff_bps": { "count": 40, "mean": -5.3, "std_dev": 28.0, "min": -90, "p5": -60, "p25": -15, "median": -2, "p75": 5, "p95": 40, "max": 80 },
    "label_tp_count": 12, "label_sl_count": 15, "label_timeout_count": 13,
    "replay_exits": [ { "reason": "max_holding", "count": 21 } ], "paper_exits": [],
    "not_evaluated_exits": ["jev_direction_reversed", "continuation_probability_dropped", "force_flat_before_close"],
    "cost_long_bps": 10, "cost_short_bps": 10
  },
  "oos_paper": { "model_version": "v1", "oos_start": "...", "oos_end": "...", "oos_ev_bps": 12.0, "paper_trades": 0, "paper_mean_bps": null, "diff_bps": null }
}
```

（上記の数値は形式の例で、実績ではない。）`warnings[].code`は`take_profit_mismatch`・`stop_loss_mismatch`・`horizon_mismatch`・`cost_mismatch`・`cost_direction_asymmetry`。`comparison.diff_bps`は再生−ラベルで、平均が負ならラベルが実出口より楽観的。`oos_paper.paper_mean_bps`・`diff_bps`は決済済みポジションが無いとき`null`（画面は「データ無し」）。

### GET /api/v1/policy-proposals

Sol/Opus自己改善ループ（`architecture/overview.md` §8）の監査用読み取り専用API。`policy_proposals`の提案・レビュー・適用・ロールバック履歴を返す。UIページは持たず、外部監視・手動確認用に提供する（実際の外部AI API呼び出しの結果を追跡できるようにするため、`requirements/functional.md` FR-SELFIMPROVE-7〜9）。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `status` | string | `pending`/`approved`/`rejected`/`applied`/`rolled_back`でフィルタ（省略時は全件） |
| `limit` | integer | 件数上限（既定50、最大200） |

```json
// Output（抜粋）
{
  "items": [
    {
      "id": 42,
      "proposed_at": "2026-09-28T15:00:00Z",
      "proposed_by": "sol",
      "status": "applied",
      "proposed_changes": { "policy.long.min_probability": 0.68 },
      "backtest_result": { "baseline_expectancy": 0.31, "candidate_expectancy": 0.3165, "baseline_max_drawdown_pct": 5.0, "candidate_max_drawdown_pct": 4.83, "expectancy_delta_pct": 2.1, "max_drawdown_delta_pct": -3.4 },
      "reviewed_by": "opus",
      "review": { "verdict": "approve", "reason": "..." },
      "applied_policy_version": "sol-42",
      "applied_at": "2026-09-28T16:00:00Z",
      "rolled_back_at": null,
      "rolled_back_reason": null
    }
  ]
}
```

`backtest_result`はシャドウバックテスト（FR-SELFIMPROVE-4）の比較結果で、実行前は`null`。基準値・候補値の`baseline_expectancy`/`candidate_expectancy`/`baseline_max_drawdown_pct`/`candidate_max_drawdown_pct`と、相対変化率`expectancy_delta_pct`（`(candidate - baseline) / |baseline| * 100`。基準Expectancyが負でも改善なら正になる）・`max_drawdown_delta_pct`（`(candidate - baseline) / baseline * 100`）の計6フィールドを常に出力する。基準値（`baseline_expectancy`/`baseline_max_drawdown_pct`）が0のときは変化率を定義できないため、該当するデルタのみ`null`になる（`internal/web/handler/proposals`の`relativeDeltaPct`）。

`applied_at`（適用日時、RFC 3339）・`rolled_back_at`（FR-SELFIMPROVE-6の自動ロールバック日時、RFC 3339）・`rolled_back_reason`（ロールバック理由。劣化前後の実現Expectancy値を含む文字列）は`policy_proposals`の同名カラムの保存値で、未発生（未適用／ロールバックされていない）の場合は`null`（キーは常に出力する）。`status=rolled_back`の行で「いつ・なぜ」ロールバックされたかを本APIで確認できる（FR-SELFIMPROVE-7）。

`policy_proposals`の保存値のうちJSONとして解釈できないものがあっても、その1行のせいで一覧全体を失敗させない（古い／壊れた行が監査履歴を隠さないため）。該当行は一覧から除外せず返し、解釈できなかったフィールドだけを空にする（`proposed_changes`は`{}`、`backtest_result`/`review`は`null`。`proposed_changes`は`new_value`が1つでもJSONでなければ`{}`）。各失敗は`proposal_id`付きでERRORログに記録する。`500`になるのは`policy_proposals`の読み込み自体が失敗した場合（ストア障害）のみ。

### GET /api/v1/activity

System Activity Log向けの直近アクティビティ・キュー状況スナップショット（`requirements/functional.md` §4.15/§5.5）。`jobs`/`jev_decisions`/`kill_switch_events`を集約する読み取り専用API。新規永続テーブルは持たない。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `limit` | integer | フィード件数（既定200、1〜500。範囲外は422） |
| `queue` | string | `jobs.queue`でフィルタ（省略時は全キュー）。許容値は`market-data`/`feature-calc`/`jev-scout`/`jev-trader`/`outcome-labeling`/`analytics`の6種で、範囲外は422。`job`イベントのみが対象で、指定時は`jev_scout`/`jev_trader`/`kill_switch`イベントは含まれない |
| `type` | string | イベント種別でフィルタ: `job` / `jev_scout` / `jev_trader` / `kill_switch` / `news_feed`（ニュース取得・Luna分類の失敗。インメモリ直近50件）/ `broker_notice`（ブローカーアダプタの運用者向け通知。立花の再認証遅延・取り合い・書面未読・API版数/書面更新予告。インメモリ直近50件。省略時は全種別。範囲外は422） |

```json
// Output（抜粋）
{
  "queues": [
    { "queue": "jev-scout", "pending": 3, "running": 1, "failed_recent": 0 }
  ],
  "events": [
    { "type": "job", "timestamp": "2026-09-29T01:16:00Z", "queue": "jev-scout", "detail": "queue=jev-scout status=succeeded attempts=1", "latency_ms": 1500 },
    { "type": "jev_trader", "timestamp": "2026-09-29T01:15:00Z", "symbol": "7203", "detail": "direction=LONG confidence=0.74", "latency_ms": 820 },
    { "type": "kill_switch", "timestamp": "2026-09-29T01:10:00Z", "detail": "reason=daily_loss_limit" }
  ],
  "as_of": "2026-09-29T01:15:03Z"
}
```

`events[].queue`は`type=job`のイベントにのみ付く（`jobs.queue`の値。それ以外のイベントではキーごと省略）。`/ws/activity`の`activity_event.event`も同じ構造。

`queues[].failed_recent`は`finished_at`が`as_of`から過去1時間（固定。設定では変更できない）以内の`failed`ジョブの件数で、`/ws/activity`の`job_update.failed_recent`も同じ窓で集計する。1時間より前に失敗したジョブは含まれない（`requirements/functional/components-platform.md` FR-ACT-1）。
