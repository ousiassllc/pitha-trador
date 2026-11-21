# アーキテクチャ設計: パッケージ一覧（bootstrap・router・config・web）

`docs/architecture/overview.md` §3 のディレクトリツリーに載せていない `internal/bootstrap`・`internal/router`・`internal/config`・`internal/web` のサブパッケージを、1 行ずつの責務でまとめる（ツリーは主要パッケージのみ。載せていないものはここで補う）。責務の根拠は各パッケージの `doc.go` または先頭の `// Package` コメント。`service`・`repository`・`domain` 側は `overview/packages.md`。

共通の性質: 研究用（research-only）と書いたものは、Policy・Risk・発注・Paper ガード・Live・Jev の入力に一切ならない。`bootstrap` 配下の `*wire`・`*job` は、親（`bootstrap`）の行数上限のために切り出した組み立て役で、親が子を import し、子は依存を引数で受け取る（`overview.md` §3「サブパッケージ共通の規約」）。

## 1. `internal/bootstrap/`

| パッケージ | 責務 |
|-----------|------|
| `airolewire` | AI プロバイダー層（`config/airole`、#1080）を、組み立て役が作るクライアントにする。起動時にライブ Jev の役割の接続を保存済みの設定と秘密から 1 回決め、Sol・Opus・Luna には呼び出しごとに設定を読み直すバックエンドを渡し（#1112）、お試しのアームのプロバイダー接続を埋める。設定も秘密も書かない |
| `backupjob` | 日次の DB バックアップジョブ（`scheduler.DatabaseBackuper`）。保存先は Settings 画面の `runtime_settings` で、未設定なら同じ PC の既定フォルダ（#708・#890） |
| `dailybars` | 立花の夜間の日足バッチ（#729）。引け後に、全銘柄の日足を Settings の速度で取り（8:00〜15:30 JST は取らない）`daily_bars` に保存する。夜ごとの結果は `daily_bar_runs` に 1 行 |
| `disclosuresrcwire` | 追加の開示・ニュース源（#1096）の組み立て。EDINET の書類一覧を記録する日次メンテナンスタスクと、立花ニュースのタスクのコンストラクタ。研究用の記録のみで、既定は両方オフ |
| `disclosurewire` | 開示のスキップ記録（#935・FR-DISCLOSURE-SKIP-1〜5）の組み立て。News Ingest が取得する feed をバックグラウンドの書き手へ写し、後からバックグラウンドの評価役が判定する。記録は見るだけ |
| `evalprovider` | 実績評価（#777）を、ルーターが返す insight provider に足す。Performance 画面の実績評価と `GET /api/v1/performance/evaluation` が `actuals.Evaluator` 越しに読む |
| `exitalignjob` | Calibration 画面の「ラベルと実際の出口」（#774）の組み立て。`metalabeljob.CalibrationSource` を、同じ DB 上の `exitalign.Reader` で拡張し、`GET /api/v1/calibration/exit-alignment` に使う |
| `healthalertwire` | 取引時間帯の見張りの組み立て。市場データのアラート検出（#802）・スリープガード（#863。どちらも 08:45 の確認窓に従う）・日次のセッション記録（#1054）を一緒に動かす |
| `homeview` | Home 画面の DB を使うデータ源（#896）。`web/handler/home.Source` を、Paper 結果 MCP のツールと同じ読み取りモデル（`paperresultsmcp/query`・`proposal`）で実装し、画面と MCP が同じ値を出す |
| `jquantswire` | J-Quants 読み取りアダプタ（#1093）の組み立て。足りない日足・分足を埋める日次メンテナンスタスク`jquants`、完了した日の研究用ラベルの付け直し、Settings 画面の求めによる J-Quants 由来データの全削除（解約後の片付け） |
| `mcpwire` | Settings 画面の MCP サービスの組み立て（#792・#795）。Paper 結果 MCP 自身の設定ファイルを編集し、同じサービスがアプリと一緒に MCP のプロセスを起動し、アプリの終了時に止める |
| `metalabeljob` | メタラベル（#764・#765）の組み立て。`Job` は夜間の再学習（保存済みの triple-barrier ラベルから学習してモデルを保存。Settings の再学習時刻以降）、`PolicyOption` は同じ設定から本番のエントリー規則を配線する |
| `migrationwire` | PC 移行の移行ファイル（#1075・FR-MIGRATE-*）をルーターへ配線する。生きている DB 上の書き出し役と、次の起動で適用する取り込みの段取り。起動側（DB を開く前に取り込みを適用）は `startup.OpenDatabase` |
| `newswire` | News Ingest の feed を選び、動いているかをログに出す。`bootstrap` の行数上限のための切り出し |
| `nightlyjobs` | 研究用の日次ジョブ 2 つの組み立て。Jev 評価の日次記録（#1089、`EvaluationTask`）と、特徴量だけの研究モデルの夜間ウォークフォワード学習（#1092、`ModelTask`）。どちらも既定オフで、自分の研究用の表にだけ書く |
| `paperrisk` | Settings 画面が編集する Paper の運用資金・エクスポージャー上限（#804。`config/papersettings`）の組み立て。起動時の初期値の保存と、Risk Engine のポートフォリオ・更新ゲート・Performance 評価が使う呼び出しごとの資金の読み取り役。Paper のみ（Live は `config/risk.yaml` のまま） |
| `proposalexec` | Paper 結果 MCP の注文案（#828）のアプリ側の処理役。MCP が記録した注文案（`received`）を Paper パイプラインと同じ Risk で検査し、Paper 注文を出すか理由を記録する。取り消し依頼の処理・約定しなかった指値の失効・再起動で `processing` に残った案の拾い直しも担う |
| `proposalview` | 注文案の画面（#829）の読み取りモデル。MCP の `order_proposals_list`・`order_proposal_get`（`paperresultsmcp/proposal`）と同じものをアプリ自身の DB から読み、画面と MCP が同じ値を出す |
| `replaywire` | リプレイ評価（#1085）の組み立て。保存済みの Jev 入力をお試しのアームへ後から送り、回答を専用の研究用の表（`service/jevreplay`）に記録する。`bootstrap` の行数上限のための切り出し |
| `researchextrawire` | 任意の研究用の部品を束ねる組み立て。J-Quants（#1093）・EDINET の書類一覧（#1096）・研究用の FX 1 分足と指数（#1084）をスケジューラのメンテナンスタスクとして、リプレイ評価（#1085）をバックグラウンドの実行役として組む。すべて Settings 画面でオンにするまで動かず、API キー無しには要求しない |
| `researchfxjob` | 研究用の FX 1 分足の記録（#1084）の日次メンテナンスタスク。設定された通貨ペア（既定は USD_JPY）の完了した GMO 取引日の BID・ASK の 1 分足を、日のマーカーとともに保存する（`service/researchfx`） |
| `researchhub` | 立花 e支店のセッション 1 つを、研究用の収集どうしで共有する持ち主（#1082・#1083）。夜間の日足収集と日中の時価の記録は同じ認証 ID を使うため同じアダプタを使う。最初の `Acquire` で遅延して作成・ログインする |
| `researchindexjob` | 研究用の指数の記録（#1084）の日次メンテナンスタスク。立花の時価の記録（#1083）から、完了した取引日の指数の値（既定は日経平均 101・TOPIX 151）を `research_index_minutes` へ写し、日のマーカー（`recorded`／`unavailable`）を書く |
| `researchlabeljob` | 研究用のホライズンラベル（#1074）の日次メンテナンスタスク。完了した JST 日の全スナップショットの 30 分・60 分・引けまでのラベルを `research_horizon_labels` へ記録し、保持期間を過ぎた行を消す（`service/researchlabel`） |
| `researchquotejob` | 研究用の時価サンプル（#1083）の保持期間の処理。保持日数を過ぎた日を、ローカルのアーカイブフォルダへ退避・検証してから消す（#1072 の `market_snapshots` と同じ「退避してから削除」。`service/snapshotarchive`） |
| `researchquotes` | 市場が開いている間、1 分ごとに、kabu が見ている銘柄（と少数の追加）の立花の時価を、kabu の `market_snapshots` と並べて `research_quote_samples` に記録する（#1083。MCP の `research_quote_source_compare` で比べる）。研究用の脇の流れで、取引しない |
| `researchwire` | 立花の研究用の収集（#1082・#1083）の組み立て。夜間の日足と日中の時価の記録を、選択中のブローカーと並べて記録する。どちらも既定オフ（`tachibanasource` の Research 設定を毎分読む）で、立花のセッション 1 つを `researchhub` で共有する |
| `tachibanawatch` | 立花の監視銘柄ソース（#730）。立花にはランキング API が無いため、夜間の日足バッチ（`dailybars`）の後に、翌立会日の監視銘柄を 1 つのリスト（最大 `Config.Max` 件）として決める。保有銘柄の固定枠・運用者の手動銘柄・日足から選んだ銘柄で構成する |
| `trialview` | 「成績」＞「お試し」タブの DB を使うデータ源。`web/handler/trialresults.Source` を、MCP の `jev_trial_compare`・`jev_trial_cost`（`paperresultsmcp/query/trialquery`）と同じ集計で実装し、画面と MCP が同じ値を出す。読み取りは 1 つの読み取り専用トランザクションで SELECT のみ |
| `virtualpaperjob` | 「仮想 Paper」の記録（#1087）の日次メンテナンスタスク。有効なプロファイルごとに完了した JST 日を保存済みの `market_snapshots`・`jev_decisions` の上でリプレイし、仮想の取引を `virtual_paper_trades` に記録する（`service/virtualpaper`） |
| `wiringflow` | **テスト専用**。`bootstrap.BuildServices` の組み立てを、`cmd/desktop` と同じやり方で外から検証するブラックボックステスト。`internal/bootstrap` の行数上限のための分離（#1115） |

## 2. `internal/router/`

`router` 配下のサブパッケージは**すべてテスト専用**（`_test.go` のみ）。ルーターの本体（`router.New` など）は `internal/router` 直下にあり、各テストはそれを外から駆動する。ディレクトリ行数上限のため、機能ごとに分けている。

| パッケージ | 責務 |
|-----------|------|
| `airolesroute` | Settings 画面の「役割ごとのモデル」（#1080）のルーター単位のテスト。ページの順序（プロバイダー→役割のモデル）・パネル・保存にセッションと CSRF トークンが要ること・却下されたフォームが何も保存しないこと・保存が記録されること |
| `bucketroutes` | Settings 画面の「資産ごとの予算枠」（#824）のテスト。予算の合計が運用資金を超える保存は理由つきで拒否され監査行も残らず、有効な保存は値と監査行が残る（`opsettings.Service`・移行済みの SQLite を使用） |
| `exportroute` | `GET /api/v1/export`（#773）のルーター単位のテスト。ハンドラ自身は `internal/web/handler/export` 側 |
| `homeroutes` | `GET /`（Home 画面。リダイレクトではない）のテスト。ホームを現在のメニューにすること、30 秒更新が取るカードの断片を登録すること（#896） |
| `jevpromptroute` | Settings 画面の Jev プロンプトの節（#872）のテスト。スイッチ・候補・日次コスト上限・コストのパネル、監査行つきの明示的な昇格、状態を変える操作がセッションと CSRF トークンを要すること |
| `jevtrialroute` | Settings 画面のモデル比較お試しの節のテスト。カード・パネル（スイッチ・アーム・上限）、お試しをオンにする唯一の入口がフォームであること、状態を変える操作にセッションと CSRF トークンが要ること、却下されたフォームが何も保存しないこと |
| `mcproute` | Settings 画面の MCP の節（#792）のテスト。カード・パネルの断片・保存と鍵の操作・Setup Guard と CSRF、生成された API キーが 1 回の応答の外へ出ないこと |
| `migrationroute` | PC 移行のエンドポイント（#1075・FR-MIGRATE-*）のテスト。セッション／CSRF の保護と、書き出し→プレビュー→確定の流れ（実サービス・一時 SQLite）。ハンドラのエラー対応は `internal/web/handler/migration` 側 |
| `opsroutes` | Settings 画面の運用設定（`opsettings`）のルートのルーター単位のテスト。運用設定のルートは `WithOperationalSettings` を付けたときだけ登録されること |
| `proposalroutes` | 注文案の画面・API（#829）のルーター単位のテスト。データ源を配線しないと空の一覧を返し、配線すればその源（`paperresultsmcp/proposal` の読み取りモデル）の値を返すこと |
| `securityheaders` | セキュリティ関連のレスポンスヘッダーのルーター単位のテスト。全ルートと拒否された要求の応答にヘッダーが付くこと、CSP がインラインと eval を禁じる（Swagger のスタイルだけ例外）こと、WebSocket の接続先を指定すると `connect-src` に入ること |
| `settingsroutes` | Settings 領域（#805。#793 の管理ハブを統合）のテスト。すべての管理画面が URL で到達でき、`/settings` がページを一覧し、ページ全体で Settings の全ダイアログがちょうど 1 回描画され、メインナビの全画面へ到達できること |

## 3. `internal/config/`

Settings 画面の設定（`runtime_settings` に置くキー・既定値・検証・型つきの設定値）をまとめたサブパッケージ群。`internal/config` 直下のディレクトリ行数上限のため、設定の系統ごとに分けている。YAML や環境変数の取り込み口は持たないものが多い（`config/*.yaml` は `internal/config` 直下）。

| パッケージ | 責務 |
|-----------|------|
| `airole` | AI プロバイダー層（#1080）。AI の役割ごとに、どのプロバイダー（独自 API＝Jev の接続、または OpenRouter）をどのモデルで呼ぶかと、役割の設定と保存済みの接続の秘密から 1 つの接続を決める `Resolve`。設定は `runtime_settings` の `ai.role.<role>`（JSON） |
| `barriersettings` | triple-barrier ラベル（#760）の設定。利確幅・損切り幅（bps）・タイムアウト（分）・想定コスト（bps）。`service/calibration.Labeler` が Jev trader 判断を測る基準 |
| `bucketsettings` | Paper の資産ごとの予算枠（#824）。`risk.paper.initial_capital_jpy` のうち、株式と債券 ETF（`bond_etf`）の 2 つの枠がそれぞれどれだけ持てるか。Live の Risk Engine は読まない |
| `costsettings` | 取引コストの想定（#779）。手数料（想定元本の bps・1 注文あたりの固定額）・方向（LONG／SHORT）とセッション（ザラ場／寄り／引け）ごとのスリッページ・SHORT の貸株料。Paper の約定・バックテスト・triple-barrier ラベルのコストが使う |
| `dailyguardsettings` | デイトレのエントリー停止ルール（#753・FR-RISK-8）のしきい値。日次の損失上限・利益確保・1 日の最大取引数。値 0 か保存行なしでそのルールは無効 |
| `disclosuresrcsettings` | 追加の開示・ニュース源（#1096。EDINET の書類一覧・立花 e支店ニュース）の設定。記録のみで、すべて既定オフ |
| `jevreplaysettings` | リプレイ評価（#1085）のスイッチ。リプレイ実行役が保存済みの Jev 入力をお試しのアームへ送ってよいか。アーム・アームごとの鍵・日次コスト上限は `jevtrialsettings` を読む |
| `jevshadowsettings` | Jev プロンプトの版管理・shadow 比較（#872）のキー。ライブの Trader プロンプトの版・候補を shadow で動かすか（Paper のみ）・候補の版・shadow の日次 LLM コスト上限・上限で止まった日の記録 |
| `jevtrialsettings` | モデル比較お試し（#1059）のキー。お試しを動かすか（Paper のみ）・どのアームに聞くか（JSON の配列）・全体の日次コスト上限・アームごとに止まった日とその理由の記録。`jev.shadow.*` とは別 |
| `jquantssettings` | J-Quants 読み取りアダプタ（#1093）の設定。スイッチ・契約しているエンドポイントのプランのフラグ・1 日の要求数の上限・遡る範囲・保存済みの J-Quants データの削除の依頼（解約後の片付け） |
| `logsettings` | Settings 画面のログレベルとローカルのログ保持期間（#772）。`runtime_settings` の `config.KeyLogLevel`・`config.KeyLogRetentionDays`。環境変数の取り込み口は無い |
| `mcpwritesettings` | Paper 結果 MCP が変えてよいもの（#806・#807）。設定ツールが書ける `runtime_settings` のキーの許可リスト・その書き込みを入り切りするスイッチ・監査とツール出力のマスク規則。許可リストに無いキーは MCP から書けない |
| `metalabelsettings` | メタラベルの 2 次モデル（#759）の設定。モデルのアルゴリズムと較正方法・夜間の再学習の時刻・シグナルが取られるための最低期待値（bps。`metalabel.DecideConfig.MinEVBps`）・LLM の学習データの終わり日 |
| `orderflowsettings` | Order Flow Imbalance の窓（#750）。3 つの遡り窓の幅（既定 10 秒・30 秒・60 秒）。`service/orderflow` が OFI を合計する |
| `paperbootstrapsettings` | Paper 専用のブートストラップのエントリー（#798）のスイッチ。メタラベルのモデルが採用されるまで、全候補が Risk Engine の前でスキップされ Paper が建玉を持たずラベルが貯まらないため、オンかつ Paper のときだけ、従来のしきい値規則でエントリーする |
| `paperpolicysettings` | Policy Engine の Paper 専用の entry_quality のしきい値（#861）。エントリーに要る Jev の `entry_quality` の最低値を、Paper だけ売買別に設定する。`policy.long.min_entry_quality`・`policy.short.min_entry_quality`（yaml の既定）は Live のしきい値のまま |
| `papersettings` | Paper の運用資金とエクスポージャー上限（#804）。「口座資産に対する割合」の上限とポジションサイズの基準になる資金・`max_position_per_symbol_pct`・`max_total_exposure_pct`・`max_trade_loss_pct` |
| `promotionsettings` | 昇格チェックリスト（#1091。Paper 結果 MCP の `promotion_checklist`）の数値の基準。候補に要る共通の行数・日数・現行とのコスト込みの差の下端が 0 を超える幅・有利な日数・多重比較調整後の p 値の上限 |
| `regimesettings` | 市場レジームの検出と、モメンタム／VWAP 回帰の切り替え（#755）の設定。指数の 1 分リターンを測る窓・分類するための自己相関とボラティリティのしきい値・回帰エントリーに要る VWAP 乖離の z スコア・Paper の A/B 比較のスイッチ |
| `researchfxsettings` | 研究用の FX 1 分足の記録（#1084）のスイッチと通貨ペア。日次メンテナンスタスク`research_fx`が GMO の公開 FX 足を取って保存するか |
| `researchindexsettings` | 研究用の指数の記録（#1084）のスイッチと指数コード。日次メンテナンスタスク`research_index`が、立花の時価の記録（#1083）の指数の値を `research_index_minutes` へ写すか |
| `researchjobsettings` | 研究用のジョブ（#1089・#1092・#1094）のスイッチと保持期間。Jev 評価の日次記録（`jev_evaluation_daily`）・特徴量だけの研究モデルの夜間学習（`research_model_runs`／`research_model_predictions`）・学習用データセット書き出しツールのスイッチ |
| `researchlabelsettings` | 研究用のホライズンラベル（#1074・#1073）のスイッチと保持期間。日次メンテナンスタスク`research_labels`が記録するか、研究用のラベル行を何 JST 日分残すか |
| `researchonlysettings` | 研究専用の機能（#1089 日次評価の記録・#1092 研究モデル・#1091 昇格チェックリストの基準）の設定を 1 つにまとめ、`opsettings` が 1 つの系統として振り分けられるようにする。どれも取引の入力ではなく、スイッチは既定オフ、基準は文書化された下限より緩くできない |
| `researchrecordsettings` | スイッチ＋銘柄リストで `runtime_settings` に置く 2 つの研究用の記録設定（#1084。FX 足 `researchfxsettings` と指数 `researchindexsettings`）の共通の窓口。`opsettings` はこの 2 つへここ経由でだけ届くため、記録を足しても振り分けコードが増えない |
| `sleepguardsettings` | スリープガード（#863）のスイッチ。オンの間、取引日の見張りの時間帯（08:45 JST から引けまで）は PC をスリープさせず市場データの収集を止めない。時間外・オフ・アプリ終了時は OS の電源設定に戻る |
| `snapshotagesettings` | ランキング監視モードで、Jev Scout／Trader と Paper のエントリーが `market_snapshots` のバーを使わなくなる古さの上限（#754）。全件スキャンモードは独自の `scan.full_scan_max_snapshot_age_seconds` を持つ |
| `tachibanasource` | 立花の監視銘柄ソース（#728）の `runtime_settings`（キー・既定値・検証・型つきの設定値）。#726 の後続の子 Issue が読む。`internal/config` の行数予算のため別パッケージ |
| `watchsettings` | kabu ランキングのウォッチリストの大きさと、観察用銘柄（観察だけの銘柄）、その呼び出しを抑える日次の Jev コスト上限（#1055）の Settings 画面の設定。YAML・環境変数の取り込み口は無い |

## 4. `internal/web/`

| パッケージ | 責務 |
|-----------|------|
| `settingspages` | Settings 領域（#805。#793 の管理ハブを統合し、6 ページは #895）の登録簿。`GET /settings` が並べるページ・どの Settings ダイアログがどのページにあるか・メインメニュー外の少数の画面（稼働ログ・初回セットアップ・Swagger）へのリンクを持つ。素のデータで、テストが全体を走査して、Settings 画面が描画するすべてのダイアログに Settings のメニューから届くことを確かめられる |
