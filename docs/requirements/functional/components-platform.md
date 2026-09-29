# 機能要件: コンポーネント別機能要件（§4.10〜§4.18 基盤・運用）

`docs/requirements/functional.md` §4 から分割した章。§4.10 Scheduler / Worker / §4.11 バックテスト / §4.12 Jevキャリブレーション / §4.13 Jev RAG / §4.14 自己改善ループ / §4.15 System Activity Feed / §4.16 Luna ニュース分類・News Ingest / §4.17 環境設定 / §4.18 初回セットアップ誘導。節番号・FR-ID は分割前と同一。

### 4.10 Scheduler / Worker

- FR-SCHED-1: 以下の6 Queueで非同期処理する: market-data, feature-calc, jev-scout, jev-trader, outcome-labeling, analytics。Risk判定とPaper発注は独立Queueを持たず、`jev-trader`ジョブ内でPolicy Engineの直後に同期実行する（Risk Engineの承認なしにExecutionへ到達しない不変条件を、ジョブ間の非同期境界で崩さないため）
- FR-SCHED-2: 60秒周期でuniverse snapshot取得・特徴量算出・screen・Jev Scout enqueueを行う
- FR-SCHED-3: 15〜30秒周期でshortlist銘柄を再評価する
- FR-SCHED-4: 5〜15秒周期で保有ポジションのExit条件を評価する（`scan.held_position_interval_seconds_min/max` の範囲でランダムに揺らした周期で、保有銘柄のみ板を取得し `execution.Engine.OnSnapshot` を呼ぶ。立会時間外は行わない）
- FR-SCHED-5: Workerはポーリングごとに期限到来済みのjobを空になるまで連続処理する（1ポーリング1jobに制限しない）
- FR-SCHED-6: ハンドラ・cronトリガー・バックグラウンドgoroutineがpanicしてもプロセスを落とさず、該当jobは`failed`（`last_error`に`handler panic: ...`）として記録し、スタックトレースをログに出力する

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

- FR-ACT-1: `jobs`テーブルをキュー別（`market-data`/`feature-calc`/`jev-scout`/`jev-trader`/`outcome-labeling`/`analytics`の6キュー）に集計し、`pending`/`running`/直近`failed`件数を提供する
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

- FR-SETUP-1: JEV_API_KEY/JEV_BASE_URL/KABU_API_PASSWORDのいずれかが未設定の間、Setup Guard Middlewareは`/setup`・`POST`/`DELETE /settings/:key`・静的アセット（`/static/...`）以外の全リクエストを`/setup`へ誘導する。ページ遷移は302、HTMXリクエストは`204`＋`HX-Redirect: /setup`、`/api/v1`は`503`のJSON、WebSocketアップグレードは`403`とし、スクリプト系リクエストに`/setup`のHTML全体を返さない。判定はリクエストごとにDBを参照するため、3キーがすべて設定された次のリクエストからリダイレクトは解除される。保存済みの値を読み出せない場合は未設定として扱う
- FR-SETUP-2: `GET /setup`は必須3項目を個別の入力欄＋保存ボタン（`SecretFieldRow`）で表示し、任意項目としてSLACK_WEBHOOK_URLを表示する
- FR-SETUP-3: `/setup`の保存・削除は`POST`/`DELETE /settings/:key`（FR-SETTINGS-2/3）をそのまま使い、専用の別実装を持たない
- FR-SETUP-4: 必須3項目がすべて設定済みなら、Setup画面は完了を表示し、通常画面（`/scanner`）へ進むリンクを出す
- FR-SETUP-5: `/setup`はセットアップ完了後も直接アクセスでき、Settings画面と同様に再設定できる
