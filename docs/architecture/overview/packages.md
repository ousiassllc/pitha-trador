# アーキテクチャ設計: パッケージ一覧（service・repository・domain）

`docs/architecture/overview.md` §3 のディレクトリツリーに載せていない `internal/service`・`internal/repository`・`internal/domain` のサブパッケージを、1 行ずつの責務でまとめる（ツリーは主要パッケージのみ。載せていないものはここで補う）。§4 は、ディレクトリ・ファイルの行数上限（linterly）のために `service`・`repository`・`domain`・`logging` の下へ切り出したサブパッケージ（本番コード 1 つとテスト専用）。責務の根拠は各パッケージの `doc.go` または先頭の `// Package` コメント。`bootstrap`・`router`・`config`・`web` 側は `overview/packages-wiring.md`。

共通の性質: 研究用（research-only）と書いたものは、Policy・Risk・発注・Paper ガード・Live・Jev のプロンプト・RAG・Calibration・メタモデルの入力に一切ならない（記録と読み出しだけ）。層のimport方向と兄弟サブパッケージ間の規約は `overview.md` §3 に従う。

## 1. `internal/service/`

| パッケージ | 責務 |
|-----------|------|
| `airoles` | Settings 画面の AI プロバイダー層の application 側（#1080）。役割ごとのプロバイダー・モデル設定、各プロバイダーの鍵の有無を読み、役割の設定を保存・戻す。保存・戻しは Settings 画面だけが呼べる（MCP は書けない） |
| `backtestlab` | Paper 結果 MCP の研究用バックテスト（#1086）。1 期間・保存済みの RunConfig 群を、パラメータ集合ごとに実際のリプレイエンジンで再生し、コスト込みの結果を記録用に要約する |
| `changeguard` | Paper の成績を動かす 2 つの変更（メタラベルモデルの採用・Policy の `policy.*` しきい値の適用）の頻度を制限する（#780・FR-META-7 / FR-SELFIMPROVE-10）。立会日（`marketcalendar`）で数える |
| `datasetexport` | 版つき・秘密なしの学習用データセットの書き出し（#1094）。Jev trader 判断 1 件＝CSV（と Parquet）1 行で、判断時点の特徴量・Jev の構造化回答・ゲート結果・ラベル・shadow／お試しの回答をローカルフォルダへ書く。研究専用 |
| `disclosuresrc` | 追加の開示・ニュース源の記録（#1096）。EDINET の書類一覧（メタデータのみ）と立花 e支店のニュースを研究用の表へ残す。記録のみで取引の入力にしない |
| `evalrecord` | Jev の日次評価の記録（#1089）。JST の 1 日が終わったら、その日の trader LONG／SHORT 判断を `jev_evaluation` と同じ評価（全アーム同一行・コスト込みの triple-barrier ラベル）で評価し、コホート別の主な数値を `jev_evaluation_daily` へ保存する |
| `exitalign` | メタラベルモデルの学習基準（triple-barrier ラベル。TP/SL/タイムアウト/コスト）と Paper の実際の出口（`execution.Config`）の対応を比較する純粋な記述（#774）。Calibration 画面の「ラベルと実際の出口」の元 |
| `featuremodel` | 研究用の特徴量モデルの学習と記録（#1092・#1123）。数値の `market_snapshots` 特徴量だけ（AI・Jev 出力なし）で、LONG／SHORT を一定時間（30 分・60 分・引け）持った場合の純 bps を予測する。ridge 回帰と小さな勾配ブースティング木（`gbt`）の 2 方式 |
| `jevprompt` | Settings 画面の Jev プロンプト版管理・shadow 比較（#872）の application 側。状態（ライブ版・候補・スイッチ・日次コスト上限・コスト・昇格履歴）の読み出し、shadow のスイッチ・候補・上限の保存、明示的な昇格。MCP の `jev_shadow_cost`・`jev_prompt_promote` の GUI 側 |
| `jevreplay` | リプレイ評価（#1085）。過去のライブ Jev trader 判断の保存済み入力（`jev_decisions.state_json`。何も足さない）をお試しのアーム（`jev.trial.arms`）へ後から送り、各アームの回答を研究用の表 `jev_replay_results`／`jev_replay_runs` に記録する |
| `jevtrial` | Settings 画面のモデル比較お試しの application 側。状態（スイッチ・アーム・全体の日次コスト上限・今日と累計の計上コスト・アーム別のコスト・失敗・停止理由）の読み出しと、スイッチ・アーム・上限の保存。お試しは費用がかかり Paper 専用 |
| `metaablation` | メタモデル（M 軸）× entry_quality 足切り（Q 軸）の 2×2 比較（#1046）。DB の**コピー**の上で、却下されたモデルを保存済みパラメータから再採点する研究用ツール。取引せず、元 DB へ書かず、モデルを昇格せず、LLM を呼ばない |
| `opsettings` | Settings 画面が持つ、秘密でない運用設定の保存先（#708）。バックアップ・ログの保存先フォルダと Policy Engine／Fast Screener のしきい値などを `runtime_settings` に置く。設定ごとの検証・既定値は `config` 側 |
| `orderflow` | 銘柄ごとの Order Flow Imbalance（OFI）を最良気配の更新の流れから積算し、Feature Engine が窓（短・中・長）で読めるようにする（`functional.md` §4.1）。特徴量・フィルタの入力のみ |
| `paperresultsmcp` | Paper 結果を、リモートの Grok Bot へアプリの SQLite から返す MCP エンドポイント（#783）。許可リストの基本設定の読み書き（#806・#807）と、Paper 注文案の受付（記録のみ・#827）も担う。詳細は `docs/architecture/paper-results-mcp.md` |
| `regime` | Jev trader 判断をどの戦略の下で行うかを決める（#755）。市場指数の 1 分リターン（ボラティリティ・lag-1 自己相関）で市場レジームを分類し、Paper 比較の A/B アームへ割り当て、平均回帰レジームでは VWAP 乖離の z スコアを測る |
| `researchfx` | 研究用の FX 1 分足の記録（#1084）。設定された通貨ペアと完了した GMO 取引日ごとに、読み取り専用アダプタ（`fx/gmo`）で BID・ASK の 1 分足を取り、分で結合して日のマーカーとともに保存する |
| `researchindex` | 研究用の指数の 1 分値の記録（#1084）。指数コード（既定: 日経平均 101・TOPIX 151）ごとに、立花の時価の記録（#1083）に残った 1 分値を `research_index_minutes` へ写し、日のマーカーを書く |
| `researchjobstatus` | Settings 画面の「研究用の日次評価の記録・学習・書き出し」カード用に、2 つの研究用の夜間タスク（Jev の日次評価の記録・研究モデルの夜間学習）の稼働状況を読む。表示専用・読み取り専用 |
| `sessiongap` | 取引日の市場データ収集が不完全だった日の判定を定め（#1054）、1 日 1 回その結論を記録する。遅く始まった日・ザラ場の一部が欠けた日も「欠け」とみなす |
| `virtualpaper` | 「仮想 Paper」プロファイルのロジック（#1087）。実 Paper との Policy／Exit 設定の差分だけを持つ複数のプロファイルを、同じ保存済み `jev_decisions`・`market_snapshots` の上でリプレイし、仮想の取引を `virtual_paper_trades` に記録して同じ日で比べる |
| `watchlistsize` | 実行時のウォッチリストの 1 日ごとの大きさの記録（#920）。市場が開いている間、メモリ上のウォッチリストの件数を 1 分ごとに読み、件数と出所を `watch_list_runtime_days` に保存する（銘柄は保存しない） |

## 2. `internal/repository/`

| パッケージ | 責務 |
|-----------|------|
| `backtestrun` | Paper 結果 MCP が始める研究用バックテストの記録（#1086。`er/tables-backtest.md`）。`backtest_runs` に 1 実行 1 行（要求・コードの版・実際に覆ったデータ・各パラメータ集合の結果）。研究専用 |
| `disclosuresrc` | 追加の開示・ニュース源の永続化（#1096）。`research_edinet_documents`・`research_edinet_fetch_days`・`research_tachibana_news`・`research_disclosure_source_status`（migration 000063） |
| `evalrecord` | Jev の日次評価の研究用記録（#1089）。`jev_evaluation_daily`（JST 日・`model_id`×`question_version` のコホート・ラベルのホライズンごとの 1 行。migration 000064） |
| `featuremodel` | 研究用の特徴量モデルの永続化（#1092）。`research_model_runs`（ウォークフォワード学習 1 回 1 行）と `research_model_predictions`（予測日の trader LONG／SHORT 判断ごとの予測純 bps。migration 000064） |
| `jevreplay` | リプレイ評価の永続化（#1085）。`jev_replay_runs`・`jev_replay_results`（実行・判断・アームごとの 1 行）と、リプレイする元のライブ判断の保存済み入力の読み出し。ライブ・shadow・お試しの判断表を読む部品からリプレイ行が見えないよう、判断系リポジトリとは意図的に分けている |
| `jquants` | J-Quants 読み取りアダプタの研究用データ（#1093。migration 000062）。上場銘柄マスタ・日足・分足・（データセット, 日, 銘柄）ごとの取得済みの印・日ごとの要求の台帳。個人利用のみ・取引の入力にしない |
| `proposalstore` | `order_proposals`・`order_proposal_events` のアプリ側（#827・#828）。処理役（`bootstrap/proposalexec`）が読むものと、注文案の進み具合の記録。MCP 側（挿入・取り消し依頼）は `service/paperresultsmcp/mcpset` |
| `rejectstats` | Policy Engine がエントリーしなかった理由の集計（#896。Home 画面の「エントリーしなかった理由」）。今日の NONE の `trade_signals` を `reject_reason` の頭で分けて数える。厳密に読み取り専用 |
| `researchbars` | 立花の研究用日足収集の永続化（#1082）。`research_daily_bar_runs`（夜ごと 1 行）・`research_daily_bar_progress`（銘柄ごと 1 行）。取引側のコードは読まない（`isolation_test.go`） |
| `researchfx` | 研究用の FX 1 分足の永続化（#1084）。`research_fx_candles`（通貨ペア×分ごとに GMO の BID・ASK を結合）・`research_fx_days`（取引日ごとの取得済みマーカー。migration 000068） |
| `researchindex` | 研究用の指数の永続化（#1084）。`research_index_minutes`（指数コード×ザラ場の分ごとの値）・`research_index_days`（取引日ごとの確認済みマーカー。migration 000069） |
| `researchquote` | 研究用の時価サンプルの永続化（#1083）。`research_quote_samples`（出所×銘柄×分ごとに、立花の時価と kabu の `market_snapshots` を並べて比べるための記録。migration 000060） |
| `researchstatus` | 立花の研究用収集（`bars`・`quotes`）の現在の状態 `research_tachibana_status` の永続化。読み取り専用の MCP が「停止中・待機中・実行中・未設定」を答えられるようにする。秘密は持たない |
| `virtualpaper` | 研究用の「仮想 Paper」の永続化（#1087）。`virtual_paper_profiles`（実 Paper との設定の差分を `variant.Params` の JSON で）・`virtual_paper_trades`・`virtual_paper_days`（プロファイル×JST 日の記録済みマーカー） |
| `watchlistruntime` | 実行時（kabu ランキング主導）のウォッチリストの 1 日ごとの大きさ `watch_list_runtime_days` の永続化（migration 000070、#920）。立花の保存リストを持つ `repository/market` とは分けている |

## 3. `internal/domain/`

`internal/domain` 直下はディレクトリ行数上限（linterly）のため、型のまとまりごとにサブパッケージへ分けている。

| パッケージ | 責務 |
|-----------|------|
| `disclosuresrc` | 追加の開示・ニュース源（EDINET の書類一覧・立花 e支店のニュース）の値型（#1096）。研究専用 |
| `jevreplay` | リプレイ評価（#1085）の値型。実行 1 件と、（実行, ライブ判断, アーム）ごとの結果 1 行。お試しの行と同様に研究データであり、取引・Policy・Risk・RAG・Calibration・メタモデルの入力にならない |
| `jevshadow` | Jev の shadow 比較（#872）の値型。候補プロンプトの回答（`Decision`）と昇格の監査（`Promotion`） |
| `jevtrial` | モデル比較お試し（#1059）の値型。ライブ判断と同じ入力に対する、ライブでない判断モデル（アーム）の回答 `jev_trial_decisions` の 1 行。取引・RAG・Calibration・メタモデルの入力にならない |
| `jquants` | J-Quants 読み取りアダプタ（#1093）の値型。上場銘柄・日足・分足。個人利用のみ・取引の入力にしない |
| `marketregime` | 市場レジームと A/B アームの語彙（#755）。`internal/domain` の行数予算のため別パッケージにしてあり、`internal/domain` は import しない。`domain.JevDecision`・`domain.CalibrationMetrics` がこの型を持つ |
| `metamodel` | Calibration 画面に出すメタラベルモデルの報告（#766）と採用の記録（#780）。`internal/domain` の行数予算のため別パッケージ |
| `orderproposal` | Paper 結果 MCP の注文案の語彙。MCP（`service/paperresultsmcp`）と処理役（`bootstrap/proposalexec`）が共有する `order_proposals`／`order_proposal_events` の値と `Proposal`（#827・#828） |
| `researchfx` | 研究用の FX 1 分足の記録（#1084。`research_fx_candles`・`research_fx_days`）の値型。取引の入力にならない |
| `researchindex` | 研究用の指数の 1 分値の記録（#1084。`research_index_minutes`・`research_index_days`）の値型。取引の入力にならない |
| `researchlabel` | 研究用の固定ホライズンのラベル（#1074。`research_horizon_labels`）の値型。市場スナップショットのバーからホライズンの出口バーまで持った場合の、コスト前後のリターン |

## 4. 行数上限のために切り出したサブパッケージ

### 4.1 本番コード

| パッケージ | 責務 |
|-----------|------|
| `service/metalabel/training/walkforward` | メタラベリングの夜間再学習（`service/metalabel/training`）のデータ側（#1163）。ラベル付きの `Row`・`WalkForwardConfig`・`Split`、行を fit < 較正 < OOS に purge・embargo つきで切る `SplitWalkForward`、履歴が足りないときの `ErrInsufficientData`（と、その詳細を持つ `InsufficientDataError`）、判断日（JST）を返す `DayOf` を公開する。`walkforward.go`（分割）と `insufficient.go`（`InsufficientDataError`）に分け、`training` のディレクトリ行数上限のために別パッケージにしてある。`training.DefaultWalkForwardConfig`（既定の分割）はこの `WalkForwardConfig` を返し、`training`・`paperresultsmcp/metalabeldiag`・`metaablation/independence/fromsplit` が import する。この分割のテスト（`walkforward_test.go`・`walkforward_reach_test.go`）も同じディレクトリにある |
| `web/middleware/apierrorpage` | ブラウザのページ遷移（`Accept: text/html`かつ非HTMX・非WebSocket）で失敗した `/api/v1` の応答（4xx/5xx）を、problem+json ではなく固定メッセージの `pages.ErrorPage` へ差し替えるミドルウェア（`Handler`。#1196）。`router.useMiddleware` が `Recovery` の直後・`WebSocketBase`/`Session`/Setup Guard の外側に置く。`internal/web/middleware` のディレクトリ行数上限のために別パッケージにしてある。JSON・HTMX・成功応答は変えない |

### 4.2 テスト専用（`_test.go` のみ）

どれも `package <名前>_test` の外部テストだけを持ち（本番コードは無い）、対象パッケージの公開 API を外から駆動する。ディレクトリ行数上限のため、対象パッケージの `_test.go` を移したもの（振る舞いは変えていない）。

| パッケージ | 責務 |
|-----------|------|
| `repository/jobqueue/failedflow` | `JobRepository.ListFailedAfter`（失敗ジョブの窓・並び順・ページングと、上限が正でないときの扱い）のテスト（#1178） |
| `repository/judgement/proposalflow` | `ProposalRepository`（Policy 提案）の挿入・取得・バックテスト結果とレビューの更新・適用済み／ロールバックの印・状態別の一覧・最後の変更時刻のテスト（#1178） |
| `repository/market/instrumentflow` | `InstrumentRepository`（作成・取得・更新・有効な銘柄の一覧・同じ銘柄の重複の拒否・直近の 5 分リターン・種別と資産クラス）のテスト（#1178） |
| `repository/metalabel/skippedflow` | メタラベルの学習行（`ListTrainingRows`）に、ラベル未確定の見送り候補（LONG／SHORT のみ）を足す `ListTrainingRowsWithSkipped` と、`calibration_outcomes` の出所の印（入った・見送り・方向なし）のテスト（#1178） |
| `repository/sqlitedb/migrationflow` | マイグレーション 000021（保存時刻を固定幅へそろえる）・000029（クローズ済みポジションの含み損益を 0 に戻す）の適用結果のテスト（#1178） |
| `repository/trading/orderflow` | `OrderRepository`（成行・指値の挿入と取得・約定・状態更新・銘柄別と状態別の一覧・エントリーの約定とポジション開設を 1 つにまとめる `FillEntry`・ポジションに結びつかない約定の検出）のテスト（#1178） |
| `domain/domainflow` | `internal/domain` の純粋ロジックのテスト。Policy 提案の検証・符号化（`ValidatePolicyChanges` ほか）と、スキャンの見送り理由（`ScreenReasons`）・スキャンサイクルの問い合わせ（`ScanCycle.Query`・`Summary`）（#1179） |
| `logging/maskflow` | `internal/logging` の秘密のマスク（`MaskJSON`・`MaskString`・`MaskJSONText`・`IsSensitiveKey`。立花の認証情報のキーを含む）のテスト（#1179） |
| `service/updater/checkflow` | `updater` のダウンロードの堅牢化（`hardening_test.go`）・`Status` の分類（`status_kind_test.go`）・ダウンロード後に安全ゲートを評価し直す再確認（`recheck_test.go`）のテスト（#398・#1179） |
| `service/opsettings/brokerflow` | `opsettings.Service` が保存するブローカー設定（`broker.*` の値の正規化・不正値の拒否・秘密鍵ファイルのパスの検証・`LoadBroker` の既定値と読み込み失敗時の扱い）のテスト（#1180） |
| `service/scheduler/periodicflow` | Scheduler の定期確認（操作者ハートビート・Kill Switch のリスク監視と自動再開・イベント再評価の投入）のテスト（#1173） |
| `service/policy/handlerflow` | `policy.Handler` の `HandleJob`（薄い板の流動性・古いスナップショットの見送り・Jev 回答の失敗・シグナルの保存・メタルール）のテスト（#1173） |
| `service/jev/shadowflow` | Jev の shadow 比較（#872）のテスト。shadow の実行（ライブと同じ入力・トークンと費用の記録）・スロットル・日次コスト・昇格の監査・ライブ版の読み出し・Paper のみで既定オフ・取引／RAG／Calibration／学習から切り離されていること（#1173） |
| `service/metalabel/metalabelflow` | メタラベルの学習器（`Train`・`FitCalibrator`・`ParseModel`）と報告役（`Reporter`。最新モデル・OOS 指標・採用の記録・再学習の見送りの集計）のテスト（#1161・#1163） |
| `service/calibration/labelertests` | Calibration の `Labeler`（triple-barrier とホライズンのラベル付け）と、ラベルから読む指標（Brier・信頼度帯・ホライズン別）のテスト（#1162） |
| `service/featureengine/featuretests` | Feature Engine の特徴量計算（`Compute`・窓リターン・OFI・スクリーン信号・足の古さ・全件スキャンの間隔）のテスト（#1162） |
