# 機能要件

## 1. ユースケース一覧

| ID | ユースケース | 主アクター | 概要 |
|----|------------|-----------|------|
| UC-1 | 市場スキャン | Scheduler | 対象ユニバースを周期的にスキャンし、特徴量を算出する |
| UC-2 | 候補絞り込み | Fast Screener | 数値条件・スコアで候補銘柄を機械的に絞り込む |
| UC-3 | 深掘り判定 | Jev Scout | 候補銘柄が今分析する価値があるかを判定する |
| UC-4 | 売買方向判定 | Jev Trader | Scout通過銘柄の方向・レジーム・エントリー品質を判定する |
| UC-5 | 取引候補生成 | Policy Engine | Jev出力をルールでトレードシグナルへ変換する |
| UC-6 | リスク検証・拒否 | Risk Engine | ポジションサイズ・損失上限・取引禁止条件を強制する |
| UC-7 | Paper発注・約定 | Execution | Paper Entry/Exitを実行し、ポジション・PnLを更新する |
| UC-8 | 判断ログ確認 | 個人トレーダー | Symbol Detail画面でJevの判断根拠・履歴を確認する |
| UC-9 | 実績確認 | 個人トレーダー | Performance画面でPnL・勝率・期待値等を確認する |
| UC-10 | キャリブレーション確認 | 個人トレーダー | Calibration画面でconfidence帯別の的中率・平均リターンを確認する |
| UC-11 | Kill Switch操作 | 個人トレーダー | UIまたはサーバーから新規取引停止・強制決済を行う |
| UC-12 | バックテスト実行 | 個人トレーダー | Paper Trading開始前に過去データで戦略を検証する |
| UC-13 | システムアクティビティ確認 | 個人トレーダー | Log画面でジョブキュー実行状況・直近のJev呼び出し・Kill Switch関連イベントをリアルタイムに確認する |
| UC-14 | 環境設定 | 個人トレーダー | Settings画面でJev/kabuステーション/Slack/Luna/Sol/Opus/ニュースフィードの認証情報をキー単位で保存・削除する |
| UC-15 | 初回セットアップ | 個人トレーダー | 必須認証情報（Jev/kabuステーション）が未設定のとき、Setup画面へ誘導され、入力を完了してから通常画面へ進む |

```mermaid
graph TD
    User((個人トレーダー))
    Scheduler((Scheduler))

    Scheduler --> UC1[市場スキャン]
    UC1 --> UC2[候補絞り込み]
    UC2 --> UC3[深掘り判定]
    UC3 --> UC4[売買方向判定]
    UC4 --> UC5[取引候補生成]
    UC5 --> UC6[リスク検証・拒否]
    UC6 --> UC7[Paper発注・約定]
    UC7 --> UC10[キャリブレーション確認]

    User --> UC8[判断ログ確認]
    User --> UC9[実績確認]
    User --> UC10
    User --> UC11[Kill Switch操作]
    User --> UC12[バックテスト実行]
    User --> UC13[システムアクティビティ確認]
```

## 2. 主要処理フロー

```mermaid
sequenceDiagram
    participant SCH as Scheduler
    participant MD as Market Data Client
    participant FE as Feature Engine
    participant FS as Fast Screener
    participant JS as Jev Scout
    participant JT as Jev Trader
    participant PE as Policy Engine
    participant RE as Risk Engine
    participant EX as Execution
    participant DB as SQLite

    SCH->>MD: universe snapshot取得（kabuステーションAPI）
    MD->>FE: 生データ
    FE->>FS: 特徴量
    FS->>FS: screen_score算出・上位N銘柄選定
    FS->>JS: 候補銘柄（50〜200）
    JS->>JS: interesting_now / liquidity_ok / abnormal_activity
    JS->>DB: jev_decisions（scout）保存
    JS->>JT: 通過銘柄（10〜30）
    JT->>JT: direction / regime / entry_quality / toxic_flow
    JT->>DB: jev_decisions（trader）保存
    JT->>PE: Jev出力
    PE->>PE: LONG/SHORT/NONE判定・しきい値評価
    PE->>DB: trade_signals保存
    PE->>RE: トレードシグナル
    RE->>RE: ポジションサイズ・損失上限・Kill Switch判定
    alt Risk承認
        RE->>EX: 発注許可
        EX->>EX: Paper Entry（kabu発注シミュレーション）
        EX->>DB: paper_orders / positions保存
    else Risk拒否
        RE->>DB: trade_signals.reject_reason保存
    end
    EX->>DB: 保有中はExit条件を継続評価
    EX->>DB: Exit時にcalibration_outcomes紐付け用データ蓄積
```

## 3. 対象市場とスコープ

- 対象: 東証上場銘柄、現物またはPaper Trading
- 基本時間軸: 1分足
- MVP戦略: 短期モメンタム / 出来高急増 / ブレイクアウト / VWAP乖離からの継続・反転
- 想定保有時間: 最短数分、基本5〜30分、原則として日跨ぎしない

## 4. コンポーネント別機能要件

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
- FR-RISK-5: すべてのRisk拒否・Kill Switch発動・自動再開を監査ログ（`kill_switch_events`）に記録する
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

### 4.10 Scheduler / Worker

- FR-SCHED-1: 以下のQueueで非同期処理する: market-data, feature-calc, jev-scout, jev-trader, risk-check, paper-execution, outcome-labeling, analytics
- FR-SCHED-2: 60秒周期でuniverse snapshot取得・特徴量算出・screen・Jev Scout enqueueを行う
- FR-SCHED-3: 15〜30秒周期でshortlist銘柄を再評価する
- FR-SCHED-4: 5〜15秒周期で保有ポジションのExit条件を評価する

### 4.11 バックテスト

Paper Trading開始前に最低限以下を検証する。

- FR-BT-1: 取引回数、勝率、平均利益、平均損失、Profit Factor、Expectancy、Max Drawdown、スリッページ込みPnL、手数料込みPnLを算出する
- FR-BT-2: Training/Calibration → Validation → Forward periodのWalk Forwardを繰り返す（全期間一括最適化しない）
- FR-BT-3: 特徴量は判定時点までのデータのみで生成する（look-ahead防止）

### 4.12 Jevキャリブレーション

- FR-CAL-1: すべてのJev判定について `state` / `decision` / `outcome` の3要素を保存する
- FR-CAL-2: 評価指標としてBrier Score、Log Loss、Expected Calibration Error、Reliability Curve、方向別平均リターン、confidence bucket別PnLを算出する
- FR-CAL-3: confidence帯（0.50-0.60, 0.60-0.70, 0.70-0.80, 0.80-0.90, 0.90-1.00）ごとに方向一致率と平均future returnを算出する
- FR-CAL-4: 判定水平線（horizon）ごとに`future_return`, `max_adverse_excursion`, `max_favorable_excursion`, `was_direction_correct`を`calibration_outcomes`に保存する

### 4.13 Jev RAG（経験ベース文脈拡張）

Jev Scout/Traderが「今の状態」だけでなく「過去の類似局面で何が起きたか」を踏まえて判断できるよう、過去データを検索し文脈として注入する。

- FR-RAG-1: `market_snapshots`（全スナップショット）および`jev_decisions`（判断が発生した局面。`calibration_outcomes`と紐付く）の各行に、標準化済み特徴量ベクトル（return_1m/5m/15m, price_vs_vwap_bps, volume_ratio_1m/5m, spread_bps, orderbook_imbalance, realized_vol_5m/15m, volatility_expansion_ratio, market_return_5m, sector_return_5m 等）を`market_snapshot_vectors`/`jev_decision_vectors`（sqlite-vec `vec0`仮想テーブル）に保存する
- FR-RAG-2: Jev Scout/Trader呼び出し直前に、現在の状態ベクトルに対しsqlite-vecで類似度上位k件（初期値k=5）を`jev_decisions`（`calibration_outcomes`紐付き済みのもの優先）および`market_snapshots`から検索する
- FR-RAG-3: 検索結果（類似局面の方向・regime・実際のfuture_return・was_direction_correct等の要約）をJevへのプロンプトにfew-shot文脈として注入する。埋め込みはLLM API呼び出しを伴わない数値特徴量ベクトルのみを用い、追加のAPIコスト・レイテンシを発生させない
- FR-RAG-4: 蓄積データが不十分な期間（コールドスタート）はRAG文脈を空のまま呼び出す（Jevの通常判断のみで動作する）
- FR-RAG-5: Symbol DetailのDecision historyに、参照した類似局面の件数を付加情報として表示できる（`components/overview.md`参照。UI必須要件ではない）

### 4.14 自己改善ループ（Luna / Sol / Opus 連携）

MVP必須要件ではないが、Phase 6（Continuous Loop）の一部として組み込む。「自己学習しながら継続的に改善する」ことを目的とし、高頻度の売買判断（Jev）とは分離した低頻度の振り返り・改善提案・検証・適用ループを構成する。Luna/Sol/Opusはいずれも実際の外部AI API呼び出しとして実装する（コード側のルールのみに依存しない、`architecture/overview.md` §8）。

| 役割 | 用途 | 呼び出し頻度 |
|------|------|------------|
| Luna（Sense） | ニュース分類・決算要約・bullish/bearish/neutral分類・イベント抽出 | リアルタイム補助（高頻度ループ内） |
| Jev（Decide） | 個別銘柄の売買方向・レジーム判断（RAG文脈込み） | 高頻度（§4.4, §4.5） |
| Sol（Think） | 負けトレード分析・相場環境変化分析・Jev誤判定クラスタ分析を行い、Policy Engineしきい値の改善提案（rationale付き）を生成する | 低頻度（日次、引け後） |
| Opus（Govern） | Solの改善提案をレビューし、直近の実績データでシャドーバックテスト検証した上で承認/却下する | Sol提案発生時のみ |
| Risk Engine（Control） | ポジションサイズ・損失上限等の最終拒否権。Sol/Opusからは変更不可 | 常時 |
| Execution（Act） | 発注・約定 | 常時 |

- FR-SELFIMPROVE-1: Sol は日次（引け後）に、直近の負けトレード・Calibration指標（Brier Score/ECE/confidence bucket別PnL）を分析し、`policy_proposals`に改善提案（`rationale_json`, `proposed_changes_json`）を記録する
- FR-SELFIMPROVE-2: Solが変更を提案できる対象は`runtime_settings`の`policy.*`キー（Policy Engineのしきい値）に限定する。`risk.*`キー（Risk Engineのリミット値）および Jev の`prompt_version`/質問セット自体は自己改善ループの対象外とし、人手のみが変更できる（`overview.md` 非目標「AIによるリスクルール変更」を継続遵守）
- FR-SELFIMPROVE-3: 1提案あたりの変更幅は confidence系しきい値で±0.05、entry_quality等の段階型しきい値で1段階までを上限とする
- FR-SELFIMPROVE-4: Opusは提案を受け取ると、直近の`trade_signals`/`jev_decisions`/`calibration_outcomes`（直近20営業日相当）に対し提案後しきい値を適用した場合のExpectancy・Max Drawdownをシャドーバックテスト（バックテストエンジン§4.11を再利用）で算出し、既存policy_versionに対しExpectancyが悪化せずMax Drawdownの悪化が許容範囲内（相対10%以内）の場合のみ承認する
- FR-SELFIMPROVE-5: 承認された提案は新しい`policy_version`として`runtime_settings`に自動適用し、`policy_proposals.status`を`applied`に更新する。却下時は`rejected`として理由を記録する
- FR-SELFIMPROVE-6: 適用後5営業日相当のExpectancyが適用前より相対20%以上悪化した場合、自動的に直前の`policy_version`へロールバックし、Slack通知する
- FR-SELFIMPROVE-7: Sol/Opusの提案・レビュー・適用・ロールバックはすべて`policy_proposals`と`runtime_settings`の変更履歴として監査可能な形で保存する
- FR-SELFIMPROVE-8: Solが生成した`proposed_changes_json`は、`selfimprove`サービスがFR-SELFIMPROVE-2（対象キーは`policy.*`のみ）・FR-SELFIMPROVE-3（変更幅上限）を機械的に検証する。逸脱する提案は`policy_proposals.status=rejected`（`review_json.reason=llm_output_out_of_bounds`）として却下し、LLM出力の内容を無条件に信用しない
- FR-SELFIMPROVE-9: Opusの承認判定は、既存の決定的シャドーバックテストしきい値（FR-SELFIMPROVE-4）とOpus APIによる定性レビューの両方を満たした場合にのみ`approved`とする。Opus APIは決定的しきい値を満たす提案を追加で却下できる（安全側の拒否権）が、決定的しきい値を満たさない提案を承認へ覆すことはできない

### 4.15 System Activity Feed

Scheduler/Jev/Risk Engineが「現在何を実行しているか」をUIから確認できるよう、既存テーブル（`jobs`, `jev_decisions`, `kill_switch_events`）を集約したリアルタイムフィードを提供する。Calibration/監査で使う既存データの保持方針（`non-functional.md` §5.1のログローテーション、各テーブル自体の保持期間）は変更しない。新規の永続テーブルは追加しない。

- FR-ACT-1: `jobs`テーブルをキュー別（`market-data`/`feature-calc`/`jev-scout`/`jev-trader`/`risk-check`/`paper-execution`/`outcome-labeling`/`analytics`）に集計し、`pending`/`running`/直近`failed`件数を提供する
- FR-ACT-2: `jobs`の状態遷移、`jev_decisions`の新規登録（Scout/Trader呼び出し）、`kill_switch_events`の発生を時刻順にマージした単一のアクティビティフィードを提供する
- FR-ACT-3: フィードの1回の取得・配信件数はデフォルト200件、`limit`クエリで最大500件まで指定可能とする。この上限はSystem Activity Log画面向けの表示制限であり、参照元テーブル（`jobs`/`jev_decisions`/`kill_switch_events`）自体の保持期間・行数には影響しない（既存のCalibration・監査用途を継続利用できるようにするため）
- FR-ACT-4: 新規イベント発生時にWebSocket（`api/endpoints.md` `/ws/activity`）でリアルタイムに配信する。初期表示は`GET /api/v1/activity`のスナップショットを用いる

### 4.16 Luna ニュース分類・News Ingest

News Ingest（`internal/service/newsfeed`）が対象銘柄に関連するニュース見出し・本文を外部ニュースフィードから取得し、Luna（外部AI API）へ送信して市場コンテキストを補強する。永続化は既存カラムの範囲内で行い、新規テーブルは追加しない。

- FR-LUNA-1: News Ingestは`instruments`テーブルの`is_active`銘柄を対象に、設定可能な外部ニュースフィード（`environment/setup.md`の`NEWS_FEED_URL`/`NEWS_FEED_API_KEY`）から見出し・本文を定期取得する
- FR-LUNA-2: 取得したニュース1件ごとにLuna API（`LUNA_API_KEY`/`LUNA_BASE_URL`）へ送信し、`sentiment`（bullish/bearish/neutral）・`event_type`（決算/業績修正/M&A/規制/その他）・`summary`を受け取る
- FR-LUNA-3: Luna応答は当該銘柄の直近ニュース文脈としてインメモリキャッシュ（直近N件、TTL付き、DB非永続）に保持し、Jev Scout/Trader呼び出し時に`jev_decisions.state_json`（既存カラム）内の`news_context`フィールドとして注入する。これによりFR-SCAN-1の「ニュースフラグ発生」トリガーを実装する
- FR-LUNA-4: Luna API失敗時はニュースフラグを立てず、通常のFast Screener/Jevフローに影響を与えない（Jev同様、失敗時は機能低下のみでシステム全体を止めないフェイルセーフ）
- FR-LUNA-5: Lunaの分類結果はJevの判断そのものを上書きしない。あくまでJev Scout/Traderへの補助的な文脈情報としてのみ用いる（`architecture/overview.md` §8「Lunaは高頻度側の補助コンポーネント」の方針を継続）

### 4.17 環境設定（Settings）

Settings画面（`GET /settings`）で認証情報を管理する。値は`secrets`テーブルにAES-256-GCMで暗号化して保存する。

- FR-SETTINGS-1: 設定項目は`internal/config`の許可キー一覧（JEV_API_KEY/JEV_BASE_URL/KABU_API_PASSWORD/SLACK_WEBHOOK_URL/LUNA_API_KEY/LUNA_BASE_URL/NEWS_FEED_URL/NEWS_FEED_API_KEY/SOL_API_KEY/SOL_BASE_URL/OPUS_API_KEY/OPUS_BASE_URL）に限定する。画面は項目ごとに`SecretFieldRow`を表示し、各行が独立した保存・削除フォームを持つ
- FR-SETTINGS-2: `POST /settings/:key`は指定キー1件のみを保存し、他キーの値に一切影響しない。空値の送信は400とし、保存済みの値を空入力で消すことはできない
- FR-SETTINGS-3: `DELETE /settings/:key`は指定キー1件のみを削除し、他キーの値に一切影響しない。許可キー一覧に無いキー名は保存・削除とも400を返す
- FR-SETTINGS-4: 保存済みの値は画面に再表示せず「設定済み」バッジのみ表示する。全ページ共通バナー（`GET /system/secrets-status`）は任意キー（SLACK_WEBHOOK_URL等）の未設定のみ案内する（必須キーはSetup Guard、§4.18が`/setup`へ誘導する）。設定変更の反映にはアプリ再起動が必要

### 4.18 初回セットアップ誘導（Setup）

必須認証情報が未設定のままアプリを使い始められないよう、専用のSetup画面（`GET /setup`）へ強制的に誘導する。

- FR-SETUP-1: JEV_API_KEY/JEV_BASE_URL/KABU_API_PASSWORDのいずれかが未設定の間、Setup Guard Middlewareは`/setup`・`POST`/`DELETE /settings/:key`・静的アセット（`/static/...`）以外の全リクエストを`/setup`へ302リダイレクトする。判定はリクエストごとにDBを参照するため、3キーがすべて設定された次のリクエストからリダイレクトは解除される。保存済みの値を読み出せない場合は未設定として扱う
- FR-SETUP-2: `GET /setup`は必須3項目を個別の入力欄＋保存ボタン（`SecretFieldRow`）で表示し、任意項目としてSLACK_WEBHOOK_URLを表示する
- FR-SETUP-3: `/setup`の保存・削除は`POST`/`DELETE /settings/:key`（FR-SETTINGS-2/3）をそのまま使い、専用の別実装を持たない
- FR-SETUP-4: 必須3項目がすべて設定済みなら、Setup画面は完了を表示し、通常画面（`/scanner`）へ進むリンクを出す
- FR-SETUP-5: `/setup`はセットアップ完了後も直接アクセスでき、Settings画面と同様に再設定できる

## 5. 画面別機能（Wails デスクトップアプリ）

```mermaid
stateDiagram-v2
    [*] --> ScannerDashboard
    ScannerDashboard --> SymbolDetail: 銘柄選択
    SymbolDetail --> ScannerDashboard: 戻る
    ScannerDashboard --> Performance: メニュー
    ScannerDashboard --> Calibration: メニュー
    ScannerDashboard --> ActivityLog: メニュー
    Performance --> ScannerDashboard: メニュー
    Calibration --> ScannerDashboard: メニュー
    ActivityLog --> ScannerDashboard: メニュー
    ScannerDashboard --> KillSwitchConfirm: Kill Switch操作
    KillSwitchConfirm --> ScannerDashboard: 確定/キャンセル
```

### 5.1 Scanner Dashboard

表示項目: Symbol, Price, 1m/5m Return, Volume Ratio, VWAP距離, Spread, Jev Direction, Jev Confidence, Entry Quality, Current Position。候補銘柄更新周期（15〜30秒）に応じてライブ更新する。

### 5.2 Symbol Detail

チャート（Price/VWAP/Volume）、Jev判定（Direction/Confidence/Regime/Entry Quality/Toxic Flow/Liquidity Stress）、Risk（Allowed Position/Stop/Take Profit）、Decision historyを表示する。

### 5.3 Performance

Total PnL, Daily PnL, Win Rate, Profit Factor, Expectancy, Max Drawdown, Average Hold Time, Sharpe/Sortino参考値, Signal countを表示する。

### 5.4 Calibration

confidence帯（0.50-0.60 〜 0.90-1.00）ごとの実方向一致率、平均future returnを表示する。

### 5.5 System Activity Log

表示項目: キュー別（8キュー）の`pending`/`running`/直近`failed`件数、直近アクティビティ一覧（時刻・種別 [job/jev_scout/jev_trader/kill_switch]・対象銘柄・詳細・latency_ms）、直近Kill Switchイベント。キュー種別・イベント種別でフィルタ可能とする。新規イベント発生に応じて`/ws/activity`経由でライブ更新する（§4.15）。

## 6. MVPフェーズ

| Phase | 内容 |
|-------|------|
| Phase 0: Data | 市場データ取得（kabuステーションAPI）、1分足保存、Feature Engine構築 |
| Phase 1: Scanner | Fast Screener、Scanner Dashboard、Top候補表示 |
| Phase 2: Jev Scout | Jev API接続、Scout Questions実装、Decision Log保存 |
| Phase 3: Jev Trader | LONG/SHORT/NONE判定、Policy Engine |
| Phase 4: Paper Trading | Entry/Exit、Position管理、Paper約定、PnL |
| Phase 5: Calibration | Outcome Labeling、Confidence bucket分析、Brier/Log Loss、RAG用embedding索引構築（§4.13） |
| Phase 6: Continuous Loop | Event-driven refresh、Open position monitoring、Kill Switch、Alert、Sol/Opusによる自己改善ループ（§4.14） |
| Phase 7: Small Live | 十分な検証後、法令・証券会社API規約を確認した上でごく小さなサイズから検討 |

## 7. MVP完了条件

- 対象銘柄を自動取得できる（kabuステーションAPI経由）
- 60秒周期でスキャンできる
- Fast Screenerで候補を絞れる
- Jev Scout / Traderを自動実行できる
- 判断結果をDB保存できる
- Risk Engineが拒否権を持つ
- Paper Tradingが動作する
- Exitが自動実行される
- PnLが集計される
- Jev判断と将来リターンを紐付けられる
- Calibration Dashboardが表示される
- Kill Switchが動作する
- RAGが類似局面をJevへの文脈として注入できる（§4.13）
- Sol/Opusの自己改善提案がシャドーバックテストで検証され、承認された場合のみ自動適用・監査ログ記録される（§4.14、Phase 6スコープ）
- System Activity Log画面でジョブキュー実行状況・直近アクティビティをリアルタイム確認できる（§4.15）

## 8. 実装時の最初の成功基準

「儲かるAI」を作ることを最初の目標にしない。最初に検証すべき問いは以下。

> Jevを通した銘柄群は、通していない銘柄群と比べて、5分後/10分後/20分後の期待リターン分布が改善するか？

これが確認できた後にEntry、Exit、Position Sizingを最適化する。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-26 | 新規作成 | 初版 |
| 1.1 | 2026-09-26 | Risk Engine（§4.7）にPaper/Live別リミット・dead-man's switch（FR-RISK-6）・Kill Switch再開の自動/手動分類（FR-RISK-7）を追加 | Phase 7も含めた完全自動運用への方針変更 |
| 1.2 | 2026-09-26 | §4.13 Jev RAG（経験ベース文脈拡張）、§4.14 自己改善ループ（Luna/Sol/Opus連携）を追加。Phase 5/6内容とMVP完了条件を更新 | 自己学習による継続的改善を組み込む方針 |
| 1.3 | 2026-09-29 | UC-13・§4.15 System Activity Feed・§5.5 System Activity Log画面を追加。既存jobs/jev_decisions/kill_switch_eventsを集約する読み取り専用フィードとし、新規永続テーブルは追加しない | 実行中処理を可視化するログ画面の追加要望 |
| 1.4 | 2026-09-29 | §4.14にFR-SELFIMPROVE-8/9（LLM出力の機械的検証、決定的しきい値とAIレビューの併用）を追加。§4.16 Luna ニュース分類・News Ingest（FR-LUNA-1〜5）を新設。新規テーブルは追加せず既存カラム（jev_decisions.state_json等）を利用 | 現状Jevのみが実AI呼び出しであった状態の是正（AI機能実装フェーズ） |
| 1.5 | 2026-09-29 | UC-14・§4.17 環境設定（FR-SETTINGS-1〜4）を追加。Settings画面をキー単位の保存・削除へ変更 | issue #79実装 |
| 1.6 | 2026-09-29 | UC-15・§4.18 初回セットアップ誘導（FR-SETUP-1〜5）を追加。FR-SETTINGS-4の必須キー警告バナーを廃止し任意キーのみの案内へ縮小 | issue #80実装 |
