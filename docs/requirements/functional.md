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

§4は `.linterly.yml` の300行/ファイル制限のため別ファイルに分割している（節番号・FR-ID・内容は分割前と同一）。

| 節 | ファイル |
|----|----------|
| §4.1〜§4.9（Feature Engine〜状態管理） | `docs/requirements/functional/components-pipeline.md` |
| §4.10〜§4.18（Scheduler/Worker〜初回セットアップ誘導） | `docs/requirements/functional/components-platform.md` |

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

表示項目: キュー別（6キュー）の`pending`/`running`/直近`failed`件数、直近アクティビティ一覧（時刻・種別 [job/jev_scout/jev_trader/kill_switch]・対象銘柄・詳細・latency_ms）、直近Kill Switchイベント。キュー種別・イベント種別でフィルタ可能とする。新規イベント発生に応じて`/ws/activity`経由でライブ更新する（§4.15）。

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
| 1.7 | 2026-09-29 | §4.7にFR-RISK-2/FR-RISK-7の検知基準（market_data_down/jev_api_down/broker_api_error/db_write_failure/unexpected_position/fill_discrepancy）と1分周期の自動検知・自動再開を追記 | issue #93/#94/#95実装 |
| 1.8 | 2026-09-29 | §4を`docs/requirements/functional/`配下の章別ファイル（components-pipeline/components-platform）へ分割。節番号・FR-IDは変更なし | issue #119（300行/ファイル制限の形骸化解消） |
| 1.9 | 2026-10-01 | §4.4/§4.5のJev質問を公式APIの型（`noul`/`choice`）で明記。FR-SCOUT-3のコスト保存を「APIが課金額を返さないためNULL」へ訂正し、`question_version`の現行値を追記 | issue #263 |
