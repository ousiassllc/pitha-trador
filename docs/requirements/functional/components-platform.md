# 機能要件: コンポーネント別機能要件（§4.10〜§4.19 基盤・運用）

`docs/requirements/functional.md` §4 から分割した章。§4.10 Scheduler / Worker / §4.11 バックテスト / §4.12 Jevキャリブレーション / §4.13 Jev RAG / §4.14 自己改善ループ / §4.15 System Activity Feed / §4.16 Luna ニュース分類・News Ingest / §4.17 環境設定 / §4.18 初回セットアップ誘導 / §4.19 エラーログのダウンロード。節番号・FR-ID は分割前と同一。

### 4.10 Scheduler / Worker

- FR-SCHED-1: 以下の6 Queueで非同期処理する: market-data, feature-calc, jev-scout, jev-trader, outcome-labeling, analytics。特徴量の算出・永続化は`market-data`ジョブ内で原子的に実行し、`feature-calc`キューは互換用の空ハンドラ（成功するだけで処理は行わない）として残し、フルスキャンは`feature-calc`ジョブをenqueueしない（4,000銘柄で毎分4,000件の空ジョブを積まないため。以前のバージョンが残した未処理の`feature-calc`ジョブをこのハンドラが消化する）。フルスキャンの`market-data`ジョブは全銘柄分を1トランザクションでenqueueする。`jev-scout`は候補更新サイクルおよびイベント再評価（FR-SCAN-1）から、`jev-trader`は`jev-scout`ジョブから、それぞれenqueueされる。`outcome-labeling`は毎分のcron（`@every 1m`）が判定水平線（5/10/20分）を経過したJev判断を拾ってenqueueし、`analytics`は平日15:40 JSTのSol/Opus自己改善バッチ（FR-SELFIMPROVE-1）専用である（いずれも約定・Exitを契機にはしない）。Risk判定とPaper発注は独立Queueを持たず、`jev-trader`ジョブ内でPolicy Engineの直後に同期実行する（Risk Engineの承認なしにExecutionへ到達しない不変条件を、ジョブ間の非同期境界で崩さないため）
- FR-SCHED-2: 60秒周期でuniverse snapshot取得（`instruments`の有効な全銘柄の板をkabuステーションAPIから取得）・特徴量算出・screen・Jev Scout enqueueを行う。フルスキャンは`kind`を区別せず、`stock`に加えて市場コンテキスト特徴量（FR-FE-4）の算出に必要な`market_index`/`sector_index`にも同じ`market-data`ジョブを投入し、板（PUSH購読が無いためREST）取得→特徴量算出→スナップショット保存を行う。一方、PUSH購読（PushFeed）と候補更新（Fast Screener→Jev Scout）は`stock`のみを対象とする。`instruments`は起動時に銘柄マスタCSV（`environment/setup.md`「銘柄マスタの投入」）から`internal/bootstrap`が冪等にupsertする（既存行の`is_active`は変更しない）。CSVが無くDBも空の場合はスキャン対象が0件になるためエラーログに記録する
- FR-SCHED-3: 15〜30秒周期でshortlist銘柄を再評価する
- FR-SCHED-4: 5〜15秒周期で保有ポジションのExit条件を評価する（`scan.held_position_interval_seconds_min/max` の範囲でランダムに揺らした周期で、保有銘柄のみ板を取得し `execution.Engine.OnSnapshot` を呼ぶ。立会時間外は行わない）
- FR-SCHED-5: Workerはポーリングごとに期限到来済みのjobを空になるまで連続処理する（1ポーリング1jobに制限しない）
- FR-SCHED-6: ハンドラ・cronトリガー・バックグラウンドgoroutineがpanicしてもプロセスを落とさず、該当jobは`failed`（`last_error`に`handler panic: ...`）として記録し、スタックトレースをログに出力する。常駐goroutine（候補更新・保有監視・PushFeed・News Ingest・トークン再発行）は`internal/safego`で1サイクル単位にpanicを回復し、ループを継続する（保有監視は銘柄単位でも回復し、1銘柄のpanicで後続ポジションのExit評価を止めない）

### 4.11 バックテスト

Paper Trading開始前に最低限以下を検証する。

- FR-BT-1: 取引回数、勝率、平均利益、平均損失、Profit Factor、Expectancy、Max Drawdown、スリッページ込みPnL、手数料込みPnLを算出する
- FR-BT-2: Training/Calibration → Validation → Forward periodのWalk Forwardを繰り返す（全期間一括最適化しない）
- FR-BT-3: 特徴量は判定時点までのデータのみで生成する（look-ahead防止）
- FR-BT-4（再現範囲と簡略化）: バックテストおよびシャドーバックテスト（FR-SELFIMPROVE-4）は、記録済みの`market_snapshots`と`jev_decisions`（Jev Trader判断）をPolicy Engineで再生し、以下の簡略化の下で取引を再現する。ライブ運用の結果とは一致せず、Expectancy・Max Drawdownはこの前提での比較値である
  - Exit条件: 固定Stop Loss・固定Take Profit・最大保有時間のみを評価する（値は`execution.Config`のFR-EXIT-2と同一）。Trailing Stop・Jev方向反転・continuation_probability低下・VWAP逆クロス・引け前強制決済（FR-EXIT-1の残り条件）は評価しない。データ末尾まで条件が成立しない場合はデータ終端で決済する
  - Risk Engine（§4.7）は適用しない（`risk_passed`は常に真）。max_open_positions・日次損失上限・連敗上限・クールダウン・サイジングによる抑制は再現せず、銘柄ごとに同時1ポジション・損益は価格リターン（%）で集計する。ライブでは拒否・縮小される取引も集計に含まれる
  - コストモデル: スリッページは売買それぞれ片道5bps（不利な方向）、手数料は0bps（kabuステーションAPIを提供する証券会社の国内現物手数料が無料のため）で固定する。Profit Factor・Expectancy・平均利益・平均損失はコスト控除前の価格リターン、スリッページ込みPnLはスリッページのみ、手数料込みPnLは手数料のみを控除した値、Max Drawdownは両方を控除した累積リターンで算出する

### 4.12 Jevキャリブレーション

- FR-CAL-1: すべてのJev判定について `state` / `decision` / `outcome` の3要素を保存する
- FR-CAL-2: 評価指標としてBrier Score、Log Loss、Expected Calibration Error、Reliability Curve、方向別平均リターン、confidence bucket別PnLを算出する
- FR-CAL-3: confidence帯（0.50-0.60, 0.60-0.70, 0.70-0.80, 0.80-0.90, 0.90-1.00）ごとに方向一致率と平均future returnを算出する
- FR-CAL-4: 判定水平線（horizon）ごとに`future_return`, `max_adverse_excursion`, `max_favorable_excursion`, `was_direction_correct`を`calibration_outcomes`に保存する

Calibration集計（FR-CAL-2/3、`ListLabeledSamples*`・自己改善の日次分析）は`jev_decisions.question_version`で分離せず、全版の判断を混在して集計する。`question_version`は記録・表示（Decision history/Activity）用で、版を跨ぐ指標はプロンプト改訂直後に混在する点に注意

### 4.13 Jev RAG（経験ベース文脈拡張）

Jev Scout/Traderが「今の状態」だけでなく「過去の類似局面で何が起きたか」を踏まえて判断できるよう、過去データを検索し文脈として注入する。

- FR-RAG-1: `market_snapshots`（全スナップショット）および`jev_decisions`（判断が発生した局面。`calibration_outcomes`と紐付く）の各行に、標準化済み特徴量ベクトル（return_1m/5m/15m, price_vs_vwap_bps, volume_ratio_1m/5m, spread_bps, orderbook_imbalance, realized_vol_5m/15m, volatility_expansion_ratio, market_return_5m, sector_return_5m 等）を`market_snapshot_vectors`/`jev_decision_vectors`（sqlite-vec `vec0`仮想テーブル）に保存する
- FR-RAG-2: Jev Scout/Trader呼び出し直前に、現在の状態ベクトルに対しsqlite-vecで類似度上位k件（初期値k=5）を`jev_decisions`（`calibration_outcomes`紐付き済みのもの優先）および`market_snapshots`から検索する。紐付き済みの判断は、Scout判断（ラベル付与不能で全候補×毎サイクル索引される）が最近傍を占めていても候補から漏れないよう、紐付き済みのみを対象にした検索で先に最大k件取得する
- FR-RAG-3: 検索結果（類似局面の方向・regime・実際のfuture_return・was_direction_correct等の要約）をJevへのプロンプトにfew-shot文脈として注入する。埋め込みはLLM API呼び出しを伴わない数値特徴量ベクトルのみを用い、追加のAPIコスト・レイテンシを発生させない
- FR-RAG-4: 蓄積データが不十分な期間（コールドスタート）はRAG文脈を空のまま呼び出す（Jevの通常判断のみで動作する）
- FR-RAG-5（将来拡張・未実装）: Symbol DetailのDecision historyに、参照した類似局面の件数を付加情報として表示する。任意要件でありUI必須要件ではない。現状は`jev_decisions`にも`GET /api/v1/symbols/{symbol}/decisions`の応答にも参照件数を保持・出力しておらず、`components/overview.md`にも対応する記述はない。実装する場合は、RAG Context Builderが検索した件数の保持先とAPI/UI表示を本書および`components/overview.md`に追記してから着手する

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
  - 「引け後」は平日15:40 JST（大引け15:30の10分後）に固定する。cron式はホストのタイムゾーン（`time.Local`）ではなくJST固定ゾーンで解釈するため、ホストTZに依存しない（`internal/service/scheduler/selfimprove.go`）。祝日は考慮せず、祝日実行時は新規Calibrationデータなしで提案なしとなる
- FR-SELFIMPROVE-2: Solが変更を提案できる対象は`runtime_settings`の`policy.*`キー（Policy Engineのしきい値）に限定する。`risk.*`キー（Risk Engineのリミット値）および Jev の`prompt_version`/質問セット自体は自己改善ループの対象外とし、人手のみが変更できる（`docs/overview.md`「含まないもの」の「AI（Jev/Sol/Opus）による Risk Engine のリミット値そのものの変更」を継続遵守）
- FR-SELFIMPROVE-3: 1提案あたりの変更幅は confidence系しきい値で±0.05、entry_quality等の段階型しきい値で1段階までを上限とする
- FR-SELFIMPROVE-4: Opusは提案を受け取ると、直近の`trade_signals`/`jev_decisions`/`calibration_outcomes`（直近20営業日相当）に対し提案後しきい値を適用した場合のExpectancy・Max Drawdownをシャドーバックテスト（バックテストエンジン§4.11を再利用。再現範囲はFR-BT-4の簡略化に従い、Exit条件の一部・Risk Engine不適用・固定コストモデルでの比較値である）で算出し、既存policy_versionに対しExpectancyが悪化せずMax Drawdownの悪化が許容範囲内（相対10%以内）の場合のみ承認する
- FR-SELFIMPROVE-5: 承認された提案は新しい`policy_version`として`runtime_settings`に自動適用し、`policy_proposals.status`を`applied`に更新する。却下時は`rejected`として理由を記録する
- FR-SELFIMPROVE-6: 適用後5営業日相当のExpectancy（窓内にクローズ済みのポジションの`realized_pnl`平均）が適用前の同じ長さの窓より相対20%以上悪化した場合、自動的に直前の`policy_version`へロールバックし、Slack通知する。「相対20%以上悪化」は`post < pre`かつ`pre - post >= |pre| × 0.20`（適用前が負でも`|pre|`を基準にし、`pre == 0`では`post < 0`のみ）で判定し、`post >= pre`（改善・同値）では決してロールバックしない。適用前または適用後のどちらかの窓にクローズ済みポジションが0件のときは「判定不能」としてロールバックしない（しきい値の厳格化で約定が0件になっても誤ロールバックしない）。判定不能の提案は`status=applied`のまま残り、追跡窓が閉じた後も打ち切らず、以降の日次実行のたびに再評価する（窓は適用時刻を基準に固定のため、窓内のデータが揃うまで判定されない）
- FR-SELFIMPROVE-7: Sol/Opusの提案・レビュー・適用・ロールバックはすべて`policy_proposals`と`runtime_settings`の変更履歴として監査可能な形で保存する
- FR-SELFIMPROVE-8: Solが生成した`proposed_changes_json`は、`selfimprove`サービスがFR-SELFIMPROVE-2（対象キーは`policy.*`のみ）・FR-SELFIMPROVE-3（変更幅上限）を機械的に検証する。逸脱する提案は`policy_proposals.status=rejected`（`review_json.reason=llm_output_out_of_bounds`）として却下し、LLM出力の内容を無条件に信用しない
- FR-SELFIMPROVE-9: Opusの承認判定は、既存の決定的シャドーバックテストしきい値（FR-SELFIMPROVE-4）とOpus APIによる定性レビューの両方を満たした場合にのみ`approved`とする。Opus APIは決定的しきい値を満たす提案を追加で却下できる（安全側の拒否権）が、決定的しきい値を満たさない提案を承認へ覆すことはできない

### 4.15 System Activity Feed

Scheduler/Jev/Risk Engineが「現在何を実行しているか」をUIから確認できるよう、既存テーブル（`jobs`, `jev_decisions`, `kill_switch_events`）を集約したリアルタイムフィードを提供する。Calibration/監査で使う既存データの保持方針（`non-functional.md` §5.1のログローテーション、各テーブル自体の保持期間）は変更しない。新規の永続テーブルは追加しない。

- FR-ACT-1: `jobs`テーブルをキュー別（`market-data`/`feature-calc`/`jev-scout`/`jev-trader`/`outcome-labeling`/`analytics`の6キュー。`feature-calc`はフルスキャンがenqueueしないため通常0件）に集計し、`pending`/`running`/直近`failed`件数を提供する。直近`failed`件数は`finished_at`が集計時刻（`as_of`）から過去1時間（固定。設定では変更できない。`activityfeed.FailedWindow`）以内の`failed`ジョブの件数で、1時間より前に失敗したジョブは数えない。`GET /api/v1/activity`の`failed_recent`と`/ws/activity`の`job_update.failed_recent`で同じ窓を使う
- FR-ACT-2: `jobs`の状態遷移、`jev_decisions`の新規登録（Scout/Trader呼び出し）、`kill_switch_events`の発生を時刻順にマージした単一のアクティビティフィードを提供する
- FR-ACT-3: フィードの1回の取得・配信件数はデフォルト200件、`limit`クエリで最大500件まで指定可能とする。この上限はSystem Activity Log画面向けの表示制限であり、参照元テーブル（`jobs`/`jev_decisions`/`kill_switch_events`）自体の保持期間・行数には影響しない（既存のCalibration・監査用途を継続利用できるようにするため）
- FR-ACT-4: 新規イベント発生時にWebSocket（`api/endpoints.md` `/ws/activity`）でリアルタイムに配信する。ジョブ状態遷移ごとのキュー件数（`job_update`）は遷移のたびに集計せず、約0.5秒間の遷移をまとめて1回の集計で、変化のあったキューについてのみ配信する（4,000銘柄規模のフルスキャンで1分あたり数千件の遷移が起きても、ワーカー/enqueue経路を集計で塞がないため。UIへのライブ反映1秒以内の目標内）。初期表示は`GET /api/v1/activity`のスナップショットを用いる

### 4.16 Luna ニュース分類・News Ingest

News Ingest（`internal/service/newsfeed`）が対象銘柄に関連するニュース見出し・本文を外部ニュースフィードから取得し、Luna（外部AI API）へ送信して市場コンテキストを補強する。永続化は既存カラムの範囲内で行い、新規テーブルは追加しない。

- FR-LUNA-1: News Ingestは`instruments`テーブルの`is_active`銘柄を対象に、設定可能な外部ニュースフィード（`environment/setup.md`の`NEWS_FEED_URL`/`NEWS_FEED_API_KEY`）から見出し・本文を定期取得する
- FR-LUNA-2: 取得したニュース1件ごとにLuna API（`LUNA_API_KEY`/`LUNA_BASE_URL`）へ送信し、`sentiment`（bullish/bearish/neutral）・`event_type`（決算/業績修正/M&A/規制/その他）・`summary`を受け取る
- FR-LUNA-3: Luna応答は当該銘柄の直近ニュース文脈としてインメモリキャッシュ（直近N件、TTL付き、DB非永続）に保持し、Jev Scout/Trader呼び出し時に`jev_decisions.state_json`（既存カラム）内の`news_context`フィールドとして注入する。これによりFR-SCAN-1の「ニュースフラグ発生」トリガーを実装する
- FR-LUNA-4: Luna API失敗時はニュースフラグを立てず、通常のFast Screener/Jevフローに影響を与えない（Jev同様、失敗時は機能低下のみでシステム全体を止めないフェイルセーフ）
- FR-LUNA-5: Lunaの分類結果はJevの判断そのものを上書きしない。あくまでJev Scout/Traderへの補助的な文脈情報としてのみ用いる（`architecture/overview.md` §8「Lunaは高頻度側の補助コンポーネント」の方針を継続）

### 4.17 環境設定（Settings）

Settings画面（`GET /settings`）で認証情報を管理する。値は`secrets`テーブルにAES-256-GCMで暗号化して保存する。

- FR-SETTINGS-1: 設定項目は`internal/config`の許可キー一覧（JEV_API_KEY/JEV_BASE_URL/JEV_MODEL/KABU_API_PASSWORD/SLACK_WEBHOOK_URL/LUNA_API_KEY/LUNA_BASE_URL/NEWS_FEED_URL/NEWS_FEED_API_KEY/SOL_API_KEY/SOL_BASE_URL/OPUS_API_KEY/OPUS_BASE_URL）に限定する。画面は項目ごとに`SecretFieldRow`を表示し、各行が独立した保存・削除フォームを持つ。画面は接続先別の一覧（Jev / kabuステーション / Slack / Luna / ニュースフィード / Sol / Opus）で、各接続先に設定済み／一部設定済み／未設定の状態（必須キーを持つ接続先は未設定の間「必須」も）を表示し、「設定する」で開くモーダル（`<dialog>`）に、その接続先のキー・URL・モデル名を1か所にまとめる（Jev: JEV_API_KEY/JEV_BASE_URL/JEV_MODEL、Luna: LUNA_API_KEY/LUNA_BASE_URL、ニュースフィード: NEWS_FEED_API_KEY/NEWS_FEED_URL、Sol: SOL_API_KEY/SOL_BASE_URL、Opus: OPUS_API_KEY/OPUS_BASE_URL、kabuステーション: KABU_API_PASSWORD、Slack: SLACK_WEBHOOK_URL）。接続先の状態は先頭キー（認証情報）の保存有無で決まり、保存・削除に応じて一覧の状態も更新される。アップデート（`#update-panel`）とエラーログ（`#error-log-panel`）は「システム」節の項目として同じ枠組みのモーダルで開く。Luna/Sol/Opus/ニュースフィードは既定値方針（issue #273）が決まるまで「表示するが任意」として扱う。空欄は既定値を意味し、保存済みの上書き値は項目ごとの削除で既定値へ戻せる（JEV_BASE_URLの既定は`https://api.typesafe.ai`、JEV_MODELの既定は`jev-latest`）
- FR-SETTINGS-2: `POST /settings/:key`は指定キー1件のみを保存し、他キーの値に一切影響しない。値は前後空白をトリムしてから検証する。トリム後に空の値は400とし、保存済みの値を空入力で消すことはできない。URL系キー（JEV_BASE_URL/SLACK_WEBHOOK_URL/LUNA_BASE_URL/NEWS_FEED_URL/SOL_BASE_URL/OPUS_BASE_URL）は`http`/`https`スキームかつホスト非空、その他の認証情報は制御文字（改行・タブ等）を含まないことを要求し、違反は400で保存しない（HTMXはトースト、非JSはエラーページ。Setup Guardは有効な必須値でのみ解除される）
- FR-SETTINGS-3: `DELETE /settings/:key`は指定キー1件のみを削除し、他キーの値に一切影響しない。許可キー一覧に無いキー名は保存・削除とも400を返す
- FR-SETTINGS-4: 保存済みの値は画面に再表示せず「設定済み」バッジのみ表示する。モーダルは`Esc`・「閉じる」ボタン・背景クリックで閉じ、開いている間はフォーカスがモーダル内に留まり、閉じるとフォーカスは開いたボタンへ戻る。`/settings#update-panel`（ヘッダーのバージョンリンク）はアップデートのモーダルを開いた状態で表示する。全ページ共通バナー（`GET /system/secrets-status`）は任意キー（SLACK_WEBHOOK_URL等）の未設定のみ案内する（必須キーはSetup Guard、§4.18が`/setup`へ誘導する）。設定変更の反映にはアプリ再起動が必要
- FR-SETTINGS-5: kabuステーションAPIのトークン発行（`/kabusapi/token`）が失敗してもアプリは継続起動し（kabuステーション未起動の開発機でもScannerやAPIを使えるようにする）、トークンを取得できるまでバックグラウンドで自動再試行する。失敗中は全ページ共通バナー（`GET /system/marketdata-status`、`MarketDataBanner`。`Header`の`#marketdata-banner`が`load`・30秒周期で取得）に「市況データを取得できません。」と原因別の対処、「自動的に再試行します。」、設定画面（`/settings`）へのリンクを表示する。原因は公式エラーコードで区別する: 接続不可＝kabuステーション未起動／API未有効（起動と「APIシステム設定」の「APIを利用する」有効化を案内。変更後はkabuステーション再起動が必要）、`4001007`・`4001017`＝未ログイン／セッション切れ（API利用設定やAPIパスワードには言及せず、右上APIアイコンが緑か・一度ログアウトして再ログインを案内。「APIを利用する」オンでも出うる。issue #305）、`4001008`＝API利用設定未完了（「APIを利用する」の有効化を案内）、`4001013`＝APIパスワード不正（`KABU_API_PASSWORD`をkabuステーションのAPIパスワードと一致させるよう案内。本番用／検証用の取り違えに注意）、その他＝エラーコードとログ確認を案内。トークンを保持している間（復旧後を含む）はバナーを表示しない
- FR-SETTINGS-6: Settings画面の「システム」節のアップデート（モーダル内の`UpdatePanel`・`#update-panel`）は、最新リリースの確認結果が「公開リリースなし」（リリース未公開、またはリポジトリにアクセスできない404）のとき、失敗（「アップデートの確認に失敗しました。」）とは区別して「公開されているリリースが見つかりませんでした（リリースが未公開か、リポジトリにアクセスできません）。」と表示する。これは成功扱いの確認結果であり、エラーログの出力や再試行は行わない（次の周期確認で再度確認する）。リリース取得の401/403とアセット取得の401/403/404は従来どおり失敗として扱う

### 4.18 初回セットアップ誘導（Setup）

必須認証情報が未設定のままアプリを使い始められないよう、専用のSetup画面（`GET /setup`）へ強制的に誘導する。

- FR-SETUP-1: JEV_API_KEY/KABU_API_PASSWORDのいずれかが未設定の間、Setup Guard Middlewareは`/setup`・`POST`/`DELETE /settings/:key`・静的アセット（`/static/...`）以外の全リクエストを`/setup`へ誘導する。ページ遷移は302、HTMXリクエストは`204`＋`HX-Redirect: /setup`、`/api/v1`は`503`のJSON、WebSocketアップグレードは`403`とし、スクリプト系リクエストに`/setup`のHTML全体を返さない。判定はリクエストごとにDBを参照するため、2キーがすべて設定された次のリクエストからリダイレクトは解除される。保存済みの値を読み出せない場合は未設定として扱う
- FR-SETUP-2: `GET /setup`はSettingsと同じ接続先一覧・モーダルの部品で、必須2項目を持つJev（JEV_API_KEY。JEV_BASE_URL/JEV_MODELは任意）とkabuステーション（KABU_API_PASSWORD）、任意のSlack（SLACK_WEBHOOK_URL）を表示する。必須の接続先は未設定の間「必須」と表示する。Luna/Sol/Opus/ニュースフィードはセットアップ後にSettingsで設定する
- FR-SETUP-3: `/setup`の保存・削除は`POST`/`DELETE /settings/:key`（FR-SETTINGS-2/3）をそのまま使い、専用の別実装を持たない
- FR-SETUP-4: 必須2項目がすべて設定済みなら、Setup画面は完了を表示し、通常画面（`/scanner`）へ進むリンクを出す
- FR-SETUP-5: `/setup`はセットアップ完了後も直接アクセスでき、Settings画面と同様に再設定できる

### 4.19 エラーログのダウンロード

運用者がエラー発生時の調査材料（開発者への共有・Issue添付）を、ログディレクトリを手で探さずUIから取得できるようにする。既存の構造化ログ（`non-functional.md` §5.1、`internal/logging`）を読み出すだけで、新規テーブル・新規ログ出力は追加しない。

- FR-ERRLOG-1: Settings画面（`GET /settings`）に「エラーログ」節（`#error-log-panel`）を置き、対象期間（直近1/7/30/90日、既定7日）とレベル（`ERROR`のみ（既定）/`WARN`以上）を選んで「ダウンロード」できる。`/setup`には置かない
- FR-ERRLOG-2: ダウンロードは`GET /api/v1/logs/errors`（`api/endpoints/huma-api.md`）を呼ぶ。`logs/<YYYY-MM-DD>.log`と30日超の`.log.gz`アーカイブから、対象期間（UTC日付、当日を含む。ファイル名と同じ基準）のうち対象レベルのslogレコードを抽出し、時刻順のNDJSON（1行1レコード、元のslog JSONのまま）を添付ファイルとして返す。JSONとして解釈できない行・`level`が対象外の行・1MiBを超える行は含めない
- FR-ERRLOG-3: 共有を前提に、出力時のみ秘密情報をマスクする（ログファイル自体は変更しない）。(a) キー名が`api_key`/`apikey`/`api-key`/`password`/`passwd`/`token`/`secret`/`authorization`/`webhook`のいずれかを（大文字小文字を無視して）含む属性は、ネストしたオブジェクトも含め値を`"[REDACTED]"`に置換する。(b) 文字列値中の次を（大文字小文字を無視して）`[REDACTED]`に置換する（`url.Error`が`error`文字列にURL全体を含めるため）: Slack Webhook URL（`https://`/`http://`の`hooks.slack.com/services/...`）、`Bearer <値>`、`Authorization: Basic|Digest|Negotiate <値>`（`:`/`=`区切り。スキーム名は残す）、URLクエリの`token=`/`api_key=`/`apikey=`/`api-key=`/`password=`/`passwd=`/`secret=`/`authorization=`の値（`client_secret=`のように接尾一致するキーを含む）。`secrets`テーブルの値は読まず、パターン照合のみで行う。ログ出力側で秘密情報を出さない方針は維持し、マスクは多層防御とする。上記以外の形（スキームなしの`Authorization: <値>`等）は照合対象外
- FR-ERRLOG-4: 出力は合計10MiBを上限とし、新しい記録を優先して古い側から切り捨てる。切り捨てが発生した場合は応答ヘッダ`X-Pitha-Truncated: true`で示す。上限を使い切った後は、より古い日のファイルを全件は読まず、対象レベルの記録が1件でもあるか（切り捨ての有無）だけ確認して打ち切る
- FR-ERRLOG-5: 該当0件でも`200`と空ファイルを返す（フォーム送信が無反応に見えないため）。`days`/`level`が範囲外・不正なら422。ログディレクトリを読めない場合、およびエクスポータ未注入（組み立て漏れ）の場合は500（固定メッセージ。原因はslogへ。`api/endpoints.md` §7）
- FR-ERRLOG-6: 読み取り専用で、日次ローテーション・アーカイブ（`non-functional.md` §5）と並行実行できる。書き込み中の当日ファイルは読み取り時点までのスナップショットとし、末尾の不完全な行は捨てる。有効なセッションCookieを必須とし、操作者の操作として操作者ハートビート（FR-RISK-6）の更新対象になる
- FR-ERRLOG-7（実装時確認）: Wails（WebView2）が`Content-Disposition: attachment`の応答を保存できるかはWindows実機で確認して確定する。保存できない場合は`cmd/desktop`が保存ダイアログ経由で同一内容を保存する経路を追加する
