# 機能要件: コンポーネント別機能要件（§4.1〜§4.9 パイプライン）

`docs/requirements/functional.md` §4 から分割した章。§4.1 Feature Engine / §4.2 Fast Screener / §4.3 スキャン頻度・イベント駆動 / §4.4 Jev Scout / §4.5 Jev Trader / §4.6 Policy Engine / §4.7 Risk Engine / §4.8 Entry / Exit / §4.9 状態管理。節番号・FR-ID は分割前と同一。

### 4.1 Feature Engine

市場データから以下の特徴量を算出する。

| カテゴリ | 特徴量 |
|---------|--------|
| 価格 | return_1m/3m/5m/15m/30m, high_distance_5m, low_distance_5m, session_high_distance, session_low_distance |
| VWAP | vwap, price_vs_vwap_bps, vwap_slope, vwap_cross_direction |
| 出来高 | volume_1m/5m, volume_ratio_1m/5m, turnover_1m/5m |
| ボラティリティ | atr_1m/5m, realized_vol_5m/15m, volatility_expansion_ratio |
| 板・約定（取得可能な場合） | best_bid, best_ask, spread_bps, bid_depth, ask_depth, orderbook_imbalance, buy_trade_ratio, sell_trade_ratio, trade_flow_imbalance, microprice |
| 市場コンテキスト | TOPIX/Nikkei225 return_1m/5m, sector_return_5m, stock_vs_sector_relative_strength, market_breadth |

- FR-FE-1: すべての特徴量は判定時点までのデータのみで算出する（look-ahead防止、§9 バックテスト参照）
- FR-FE-2: 板・約定特徴量はkabuステーションAPIから取得できない銘柄・時間帯では欠損値として扱い、依存するJev入力/スコアから除外する

### 4.2 Fast Screener

- FR-FS-1: Jev API呼び出し前に、環境変数/DB設定値（`min_price`, `max_price`, `min_turnover_5m_jpy`, `max_spread_bps`, `min_volume_ratio`, `min_abs_return_5m_pct`, `min_realized_volatility`）で明らかに対象外の銘柄を除外する
- FR-FS-2: 通過銘柄に対し以下のスコアを算出し、上位N銘柄のみJev Scoutへ送る

```text
screen_score =
  w1 * normalized_volume_ratio
+ w2 * abs(return_5m)
+ w3 * breakout_strength
+ w4 * orderbook_imbalance
+ w5 * volatility_expansion
```

  - `breakout_strength`: 直前5分間（判定時点の足を除く）の高値/安値に対する現在価格のブレイク幅（高値上抜け時は`price/high - 1`、安値下抜け時は`1 - price/low`、レンジ内は`0`）。Feature Engine（`featureengine.ComputeScreenSignals`）が算出する
  - `volatility_expansion`: `volatility_expansion_ratio`（`realized_vol_5m / realized_vol_15m`）。同上
  - 履歴不足で算出できない項は0ではなく欠損として合計から除外する（FR-FE-2と同じ扱い）

- FR-FS-3: フィルター設定値・スコア重みはすべて環境変数またはDBで変更可能とする。優先順位は`config/strategy.yaml` < 環境変数`PITHA_FAST_SCREENER_*`（起動時に読込み。例: `PITHA_FAST_SCREENER_MIN_PRICE`, `PITHA_FAST_SCREENER_TOP_N`, `PITHA_FAST_SCREENER_WEIGHT_BREAKOUT_STRENGTH`）< DB `runtime_settings`の`screener.*`キー（候補更新周期ごとに読み込むため再起動不要。キー一覧は`architecture/er.md` §runtime_settings）

### 4.3 スキャン頻度・イベント駆動

| 対象 | 周期 |
|------|------|
| 全体スキャン | 60秒ごと |
| 候補銘柄更新 | 15〜30秒ごと |
| ポジション保有銘柄 | 5〜15秒ごと |

- FR-SCAN-1: 以下のいずれかを満たした銘柄は通常周期を待たず再評価する: 1分リターン急変、出来高急増、スプレッド急拡大、板インバランス急変、VWAPクロス、高値/安値ブレイク、約定フロー急変、ニュースフラグ発生
- FR-SCAN-2（再評価抑制）: `abs(return_1m_change) < threshold AND volume_ratio_change < threshold AND spread_change < threshold AND no_event` の場合はJev呼び出しをスキップし、APIコストとレイテンシを削減する

### 4.4 Jev Scout

- FR-SCOUT-1: 1回のJev呼び出しで以下の質問群を評価する: `interesting_now`（yes/no型）, `momentum_quality`（weak/moderate/strong/exceptional）, `liquidity_ok`（yes/no型）, `abnormal_activity`（yes/no型）
- FR-SCOUT-2: 通過条件は `interesting_now >= 0.65 AND liquidity_ok >= 0.70 AND abnormal_activity >= 0.55`（初期値。バックテスト後に調整）
- FR-SCOUT-3: 入力・出力・状態ハッシュ・レイテンシ・モデルID・コストを`jev_decisions`（decision_type=scout）に保存する

### 4.5 Jev Trader

- FR-TRADER-1: Scout通過銘柄に対し以下を評価する: `direction`（LONG/SHORT/NONE）, `regime`（TREND/RANGE/BREAKOUT/CHAOTIC）, `entry_quality`（poor〜exceptional）, `toxic_flow`（yes/no型）, `liquidity_stressed`（yes/no型）, `continuation_probability`（yes/no型）
- FR-TRADER-2: Jevのconfidence/probabilityを実際の株価上昇確率とみなさない。実結果との対応はCalibrationで独自に検証する
- FR-TRADER-3: 入出力を`jev_decisions`（decision_type=trader）に保存する

### 4.6 Policy Engine

- FR-POLICY-1: LONG条件: `direction == LONG AND P(LONG) >= 0.68 AND entry_quality >= strong AND continuation_probability >= 0.60 AND toxic_flow <= 0.35 AND liquidity_stressed <= 0.25`
- FR-POLICY-2: SHORT条件: `direction == SHORT AND P(SHORT) >= 0.68 AND entry_quality >= strong AND continuation_probability >= 0.60 AND toxic_flow <= 0.35 AND liquidity_stressed <= 0.25`
- FR-POLICY-3: 以下のいずれかに該当する場合はNONE（取引しない）: JevがNONE、確信度不足、スプレッド過大、板が薄い、Risk Engine拒否、データ欠損、API異常、キャリブレーション対象外
- FR-POLICY-4: しきい値はCalibration結果に基づき調整する。プロンプト変更より先にポリシー側のしきい値調整を優先する
- FR-POLICY-5: 生成したトレードシグナルを`trade_signals`に保存する（policy_version、risk_passed、reject_reasonを含む）

### 4.7 Risk Engine

Risk EngineはJevより優先され、Jevから変更できない。Phase 7（実売買）移行後も人手承認を挟まず自動運用することを前提とし、その代わりPaper運用よりも厳格なLive用リミットと、後述のdead-man's switchで安全側に倒す。

| 項目 | Paper初期値 | Live初期値（Phase 7） |
|------|-----------|----------------------|
| max_position_per_symbol_pct | 2.0 | 1.0 |
| max_total_exposure_pct | 20.0 | 10.0 |
| max_daily_loss_pct | 1.0 | 0.5 |
| max_trade_loss_pct | 0.25 | 0.15 |
| max_open_positions | 5 | 3 |
| max_spread_bps | 30 | 20 |
| max_consecutive_losses | 4 | 3 |
| cooldown_after_loss_minutes | 5 | 10 |
| force_flat_before_market_close_minutes | 10 | 15 |
| heartbeat_timeout_minutes（Live専用） | 対象外 | 120 |

- FR-RISK-1: 上記制限のいずれかに抵触する場合、新規取引を拒否する
- FR-RISK-2: 以下のいずれかでKill Switch（新規取引停止）を発動する: 日次損失上限到達、連敗上限到達、市場データ停止、Jev API連続失敗、Broker API異常、想定外ポジション発生、約定差異検知、DB書き込み失敗が一定回数継続、operator_heartbeat_timeout（Live専用、FR-RISK-6参照）
- FR-RISK-3: Kill Switch発動時、必要に応じて保有ポジションをクローズする
- FR-RISK-4: Kill SwitchはUI（Wailsアプリ）とサーバー内部処理の両方から操作可能とする。Phase 7の発注確定・Kill Switch操作に人手の追加認証は要求しない（完全自動運用）
- FR-RISK-5: すべてのRisk拒否・Kill Switch発動・自動再開を監査ログ（`kill_switch_events`・`kill_switch_resolutions`、追記専用）に記録する
- FR-RISK-6（dead-man's switch、Live専用）: Wailsアプリの認証済みUIリクエストを「操作者ハートビート」として記録する。立会時間中に`heartbeat_timeout_minutes`（初期値120分）を超えてハートビートが途絶した場合、Risk Engineは自動的に新規エントリーを停止する（保有ポジションのExitルールは継続）。オペレーターがUIを再度操作した時点でこの停止理由は自動解消する
- FR-RISK-7（Kill Switch再開の自動/手動分類）: `kill_switch_events.reason`により再開方法を分ける

| 発動理由 | 再開方法 |
|---------|---------|
| market_data_down（データ復旧確認後） | 自動再開 |
| jev_api_down（API復旧確認後） | 自動再開 |
| operator_heartbeat_timeout（ハートビート再検知） | 自動再開 |
| cooldown_after_loss経過（連敗後クールダウン） | 自動再開 |
| daily_loss_limit（日次損失上限到達） | 手動再開のみ |
| unexpected_position（想定外ポジション） | 手動再開のみ |
| fill_discrepancy（約定差異） | 手動再開のみ |
| consecutive_losses（連敗上限到達） | 手動再開のみ |
| db_write_failure（DB書き込み失敗継続） | 手動再開のみ |
| broker_api_error（Broker API異常） | 手動再開のみ |

FR-RISK-2/FR-RISK-7の検知・自動再開は、Schedulerが1分周期で実行する（`internal/service/scheduler`の`WithRiskMonitor`/`WithAutoResumer`）。判定基準は以下。

- market_data_down: kabuステーションAPIの板取得（`GetBoard`）が5回連続で失敗（成功1回で復旧、自動再開）
- jev_api_down: Jev APIの直近呼び出しエラー率がしきい値（既定50%、直近20件、最小5件）以上（しきい値未満に戻るか、5分間呼び出しが無ければ復旧、自動再開）。Slack通知（§5.2）と同じ信号を使う
- broker_api_error: kabuステーションAPIがHTTP 5xxを5回連続で返す（手動再開のみ）。4xx・通信エラーは対象外（通信エラーは市場データ停止側で扱う）
- db_write_failure: SQLiteの書き込みがストレージ起因（BUSY/LOCKED/READONLY/IOERR/FULL/CANTOPEN/CORRUPT）で5回連続失敗（手動再開のみ）。制約違反は対象外
- unexpected_position / fill_discrepancy: Paper Tradingでは外部Brokerが無いため、保有中ポジションを起点となる`paper_orders`の約定記録と突合する。起点注文が存在しない・未約定・銘柄/売買方向が不一致なら`unexpected_position`、約定数量・約定価格がポジションと不一致、または指値を超えた約定なら`fill_discrepancy`（手動再開のみ）
- cooldown_after_loss: クールダウンはRisk Engineの時間ベースの新規取引ゲート（FR-RISK-1、`kill_switch_events`には記録しない）であり、経過で自動的に解除される
- 日次損失上限の80%到達時のSlack通知（`non-functional.md` §5.2）も同じ1分周期の検知で行う

### 4.8 Entry / Exit

- FR-ENTRY-1: 成行想定Paper Entry・指値Paper Entryの両方を選択可能とする
- FR-ENTRY-2: 実売買へ移行する場合は原則として指値を優先する
- FR-EXIT-1: 以下のExit条件を併用する: 固定Stop Loss、固定Take Profit、Trailing Stop、Jev方向反転、continuation_probability低下、VWAP逆クロス、最大保有時間到達、引け前強制決済
- FR-EXIT-2: 初期値: `stop_loss_pct=0.6`, `take_profit_pct=1.2`, `trailing_stop_pct=0.5`, `max_holding_minutes=20`
- FR-EXIT-3: Jev API不応答時も、既存ポジションはコードベースのExit Ruleで管理を継続する（Jev不応答を理由にリスク管理を停止しない）

### 4.9 状態管理

各銘柄について以下を保持する。

```json
{
  "symbol": "XXXX",
  "last_price": 0,
  "last_scan_at": null,
  "last_jev_scout_at": null,
  "last_jev_trader_at": null,
  "last_signal": "NONE",
  "last_signal_confidence": 0,
  "position": null,
  "cooldown_until": null
}
```
