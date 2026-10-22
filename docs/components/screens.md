# 画面一覧と Settings の構成（issue #793・#805・#895）

`docs/components/overview.md` §3 の分割章。メインメニューの画面と、管理系の画面・機能の一覧、それらの入口である Settings（`GET /settings`）の構成を定める。旧「管理ハブ」（`GET /admin`、issue #793）は issue #805 で Settings に統合し、issue #895 でメインメニューを「ホーム／候補銘柄／AIの注文提案／成績／設定」の5項目に整理した。登録簿は `internal/web/settingspages`（プレーンな Go データ。Settings の描画とテストが同じ登録簿を読む）。

## 1. ナビゲーション構造

- メインナビ（`organisms.MainNav`）は左から ホーム（`/`）／候補銘柄（`/scanner`）／AIの注文提案（`/order-proposals`）／成績（`/performance`）／（区切り）／**設定（`/settings`）**（issue #895。ラベルはすべて日本語）。各ページは自分のナビ項目（`organisms.NavItem`）を `layout.Shell` に渡し、その項目だけに `aria-current="page"` が付く: ホーム = `/`（`NavHome`）／候補銘柄 = `/scanner`・`/watchlist`・`/symbols/:symbol`（`NavScanner`）／AIの注文提案 = `/order-proposals(/:id)`／成績 = `/performance`（実績・バックテスト）・`/calibration`（予測の精度）（`NavPerformance`）／設定 = `/settings`・`/settings/<page>`・`/activity`（稼働ログ。`NavSettings`）。`NavHome` はホーム項目を表す。`/` はこの版では従来どおり `/scanner` へ 302 でリダイレクトする（ホーム画面は別 Issue）ため、いまは「ホーム」が強調されるページはない。区切りは「設定」の直前の1か所。
- **タブ**: 複数ページにまたがるメニュー項目は、ページ上部のタブ（`molecules.TabNav`。各タブは独立した URL へのリンク）で行き来する。候補銘柄 = 「候補」（`/scanner`）・「監視リスト」（`/watchlist`）、成績 = 「実績」（`/performance`）・「バックテスト」（`/performance?tab=backtest`）・「予測の精度」（`/calibration`）。現在のタブに `aria-current="page"` が付く。旧ブックマーク `/performance#backtest` は URL のハッシュがサーバーに届かないため、実績タブの `pitha-performance-tabs` が `/performance?tab=backtest` へ転送する。
- **Settings の階層**: `/settings`（ページ一覧と、メインメニューにない画面（稼働ログ・初回セットアップ）へのリンク。`pages.SettingsHubPage`）→ `/settings/<page id>`（詳細設定のページ。`pages.SettingsPage`）→ 各カードのダイアログ（`molecules.Modal`）。一覧の各行（`molecules.SettingsLinkCard`）は、ページ名・要約・「開く」リンクを持つ。詳細ページは、パンくず（設定 / ページ名）・そのページに属する接続先カード・設定項目カード・システムカード・関連画面へのリンク（`settingspages.LinksOf`。メインメニューと重複する画面は置かず、稼働ログ・初回セットアップ・Swagger だけ）を並べる。カードの「開く」「設定する」ボタンでダイアログが開き、ダイアログの中で項目ごとに保存・削除する。
- ページ（この名前・順序で固定。`settingspages.Pages()`。ID は旧ページの URL を引き継ぐ）: 取引ルール `/settings/operations`（スキャン・ポジション、候補を絞るしきい値、売買判断の基準）／リスク・資金 `/settings/risk`（Paper の運用資金・建玉上限・資産ごとの予算枠（株・債券ETF。旧「資産ごとの予算枠」ページを統合）、損失上限・利益確保・売買コストの想定）／予測モデル `/settings/results`（判断のラベル・メタラベリング・相場状態・板情報）／証券会社・データ `/settings/broker`（kabu・立花の接続と監視銘柄）／外部連携・AI `/settings/integrations`（Jev・Luna・Sol・Opus・ニュースフィード・Slack・MCP。MCP のスイッチは「AIからの注文提案を受け付ける（Paper のみ）」）／システム `/settings/maintenance`（稼働ログへのリンク・アップデート・エラーログ・データエクスポート・バックアップ・ログ設定・スリープ防止）。
- **戻り導線の規則**: `layout.Shell` を使う全ページはヘッダーのメインナビに「設定」を持つため、どの画面からも1操作で Settings のページ一覧へ戻れる。詳細ページはパンくずからも一覧へ戻れる。例外は `/setup`（`SetupShell`。ヘッダーを持たない初回専用画面）と `/swagger`（静的なドキュメント画面）。ダイアログを開いたままでもヘッダーは隠れない。
- **旧 URL**: `/admin` は `/settings` へ、`/settings/asset-buckets`（旧「資産ごとの予算枠」ページ）は `/settings/risk` へ 301 でリダイレクトする。旧い `/settings#modal-<id>`（ハッシュ付きの Settings 直リンク。`#modal-asset-buckets` を含む）と `/settings#update-panel`（ヘッダーのバージョン表示の旧リンク先）は、サーバーにはハッシュが届かないため、一覧ページが埋め込む対応表（`data-hash-routes`、`settingspages.HashRoutes()`）を `pitha-settings-hub` が読み、該当ページ `/settings/<page id>#<id>` へ `location.replace` で転送する。ヘッダーのバージョン表示の現在のリンク先は `/settings/maintenance#update-panel`。各 Settings ページも同じ対応表を埋め込んで `pitha-settings-hub` を読み込み、別のページへ移ったダイアログのハッシュ（例: `/settings/operations#modal-sleep-guard` は `/settings/maintenance#modal-sleep-guard` へ）を転送する（そのページ自身のダイアログのハッシュは転送せず、そのまま開く）。他の既存 URL（`/scanner`・`/watchlist`・`/performance`・`/calibration`・`/activity`・`/order-proposals/:id`・`/symbols/:symbol`・`/settings/<page>#modal-*`）は変わらない。
- Settings の一覧・各ページは他のページと同じく Setup Guard の対象（`/setup`・`/static`・秘密キーの `POST`/`DELETE /settings/:key` のみが例外）。
- **要対応バッジ（issue #896）**: ホーム以外のページのヘッダーには、以前は任意キー未設定・アップデート・市況データ接続エラー・新規エントリー停止・市況データ停止の警告が帯として積み上がっていた。いまは「要対応 N 件」の小さなバッジ1つ（ホームへのリンク。0件なら出ない）に縮約し、全文はホームの「要対応」に出す（ホーム自身はバッジを出さない）。件数はホームの要対応一覧の件数と同じ。

## 2. 画面一覧

### 2.1 メインメニューの画面

メインナビ（ホーム／候補銘柄／AIの注文提案／成績／（区切り）／設定）から開く画面。「強調される項目」は、そのページで `aria-current="page"` が付くナビ項目。

| メニュー項目 | URL | できること | 強調される項目 |
|--------------|-----|-----------|----------------|
| ホーム | `/`（issue #896。以前は `/scanner` への 302 リダイレクト） | 今日の状況を1画面で確認します。1段目（1366×768 でスクロールなし）: **取引の状態**（「新規エントリー: 可能」または「停止中 — 理由」を1文で。理由の優先順は Kill Switch → 一時停止 → 日次ガード（理由ラベル付き）→ kabu 401・未ログイン・未起動 → 立会時間外（次回の立会開始時刻付き）→ 市況取得停止 → メタモデル未採用かつブートストラップ無効。下に既存の Kill Switch パネル）／**今日の Paper 損益**（今日（JST 0時以降）決済した分の実現損益＋保有中の含み損益＝合計、決済件数・新規エントリー件数、損失上限までの残り。上限に達したら「損失上限に達しました」）／**保有ポジション**（銘柄・売買・数量・取得価格・現在値・含み損益・保有時間の表。銘柄から銘柄詳細へ。0件は「保有中のポジションはありません」）。2段目: **要対応**（あれば赤・黄・青のカードで、各カードに「どこを見るか」リンク。無ければ「対応が必要なものはありません」の1行）。3段目（折りたたみ可）: **今日の詳細**（エントリーしなかった理由の件数、`entry_quality_below_threshold` が最多のときは使用中の最低エントリー品質（Paper/Live）と設定リンク／スキャン状況のファネル／AIの注文提案の今日の件数／最近の取引5件／モデル状態／最近の Kill Switch 3件）。各カードは30秒ごとに取り直し、読み取りに失敗したカードだけがエラー表示になります（読み取り専用。書き込みの操作は Kill Switch パネルだけで、既存の API を使います）。 | ホーム |
| 候補銘柄 ＞ 候補 | `/scanner` | スキャンの絞り込み状況と候補銘柄・Jev の判断を確認します（候補から銘柄詳細へ進めます）。タブ「候補」「監視リスト」を持ちます。 | 候補銘柄 |
| 候補銘柄 ＞ 監視リスト | `/watchlist` | 立花の監視リスト（どの銘柄をどう選んだか）を確認します。説明文の設定リンクは「設定 ＞ 証券会社・データ」（`/settings/broker`）です。 | 候補銘柄 |
| 銘柄詳細 | `/symbols/:symbol` | 銘柄のチャート・Jev の判断・Risk・保有ポジション（決済）を確認します。候補銘柄の候補から遷移するパラメータ付きの画面で、Settings のページにはリンクを置きません。 | 候補銘柄 |
| AIの注文提案 | `/order-proposals`（詳細は `/order-proposals/:id`、画面上は「注文提案 #n」・戻るリンク「← 注文提案の一覧」） | MCP の AI クライアントが出した Paper の注文提案を、期間・状態・枠で絞って一覧し、1 件ずつ理由・損切りと手じまいの計画・最大損失（あいりの見込み・アプリの見積もり・実際の損失）・情報源・状態の時系列・紐づく注文とポジションの損益を確認します（読み取り専用。承認・作成・取消の操作は無い）。詳細のトレードシグナルは、Risk Engine が拒否した案は「Risk check: 拒否」と「拒否コード」、Risk を通ったが後段（数量・最大損失・Execution）で拒否された案は「Risk check: 通過」と「後段で拒否: <コード>」を示します。 | AIの注文提案 |
| 成績 ＞ 実績 | `/performance` | 決済済みポジションの実績（Paper）と評価指標（累計損益・勝率・最大ドローダウンなど。英語の指標名は項目の `title` に残す）を確認します。タブ「実績」「バックテスト」「予測の精度」を持ちます。 | 成績 |
| 成績 ＞ バックテスト | `/performance?tab=backtest`（旧 `/performance#backtest` は `pitha-performance-tabs` がこの URL へ転送。`from` 付きの検索も同じタブ） | 期間を指定して Walk Forward バックテストを実行し、コスト感度を確認します。この版でフォームと結果は実績とは別のタブに置きます（実績は読みません）。 | 成績 |
| 成績 ＞ 予測の精度 | `/calibration` | Jev の確率予測が実際の結果とどれだけ合っているかをヒートマップで確認します。 | 成績 |
| 設定 | `/settings`（旧 `/admin` は 301） | 設定の6ページ（取引ルール／リスク・資金／予測モデル／証券会社・データ／外部連携・AI／システム）の一覧と、メインメニューにない画面（稼働ログ・初回セットアップ）へのリンクです。 | 設定 |
| 設定 ＞ システム ＞ 稼働ログ | `/activity` | システムの稼働ログとキューの状況（ジョブの状況・最近の動き・最近の Kill Switch）を確認します。 | 設定 |
| Kill Switch | ヘッダー右上（専用 URL なし）。ホームでは取引の状態カード内 | ホーム以外の全ページのヘッダー右上のパネル（ホームでは取引の状態カードの中の同じパネル）で、新規エントリーを停止・再開、全決済して停止を操作します。状態は「稼働中／新規停止中／全停止（Kill）」のバッジ1つで示し、パネルは状態の文字列を重ねて表示しません。 | — |

### 2.2 設定の画面・ダイアログ

設定の6ページの全カード（ダイアログ）。`/settings/<page>#…` の行は、そのページのカード（`molecules.SettingsCard`）が開くダイアログ（`molecules.Modal`）で、ID は `modal-<カードの id>`。立花の接続設定ダイアログ（`modal-tachibana-conn`）は、接続先カードの `modal-tachibana`（認証情報）と ID が重ならないよう運用設定グループ側の id を `tachibana-conn` としている。

| 画面名 | URL | できること | Settings のページ |
|--------|-----|-----------|------------------|
| Fast Screener | `/settings/operations#modal-screener` | 候補を絞り込むしきい値・上位件数・スコア重みを編集します。 | 取引ルール |
| Policy Engine（ロング） | `/settings/operations#modal-policy-long` | ロングのシグナルになるための確率・エントリー品質などのしきい値を編集します。 | 取引ルール |
| Policy Engine（ショート） | `/settings/operations#modal-policy-short` | ショートのシグナルになるための確率・エントリー品質などのしきい値を編集します。 | 取引ルール |
| データの鮮度（ランキング監視） | `/settings/operations#modal-snapshot-age` | Jev の判断と Paper エントリーが最新の足をどこまで古くてよしとするかの上限（秒）を編集します。 | 取引ルール |
| Paper ブートストラップ | `/settings/operations#modal-paper-bootstrap` | メタモデル未採用の間だけ Paper で旧ルールのエントリーを許し、ラベルを貯めるスイッチ（既定は無効、Live では必ず無効）を編集します。 | 取引ルール |
| スリープ防止 | `/settings/maintenance#modal-sleep-guard` | 取引日の 08:45〜大引けだけ PC を自動でスリープさせないスイッチ（既定は無効。Windows のみ。時間外とアプリ終了時は OS の設定に戻ります）を編集します。 | システム |
| キャリブレーションのラベル（トリプルバリア） | `/settings/results#modal-triple_barrier` | 判断に正解・不正解のラベルを付ける利確・損切り幅と時間切れを編集します。 | 予測モデル |
| メタラベリング | `/settings/results#modal-metalabel` | 入る／見送るを決める二次モデルの設定と最低 EV を編集します。上部に LLM cutoff の状態（仮置き／未確認／確認済み）と、公式の日付の確認手順（どこを見て何を入れるか）を表示します。 | 予測モデル |
| 相場状態（レジーム）判定 | `/settings/results#modal-regime` | 相場状態（トレンド／平均回帰）の判定と、VWAP 回帰モードへの切替を編集します。 | 予測モデル |
| 板の需給（OFI） | `/settings/results#modal-orderflow` | 板の需給（Order Flow Imbalance）を集計する窓の幅を編集します。 | 予測モデル |
| Paper の運用資金・建玉上限・1回の最大損失 | `/settings/risk#modal-paper-risk` | Paper の運用資金（ポジションサイズの分母）・1銘柄の上限・総エクスポージャの上限・1回の取引の最大損失（%）と、日次ガード用の運用資金を並べて編集します。保存は再起動不要で次のリスク判定から反映され、1単元（100株）が運用資金を超える場合は注意を表示します。運用資金を、株・債券ETFの枠の合計より下げる保存は拒否されます。Paper 専用（Live は変わりません）。 | リスク・資金 |
| 資産ごとの予算枠 | `/settings/risk#modal-asset-buckets` | Paper の運用資金を分ける、株の枠と債券ETFの枠（円。0 はその枠を使わない）を編集します。「計算結果」に運用資金・枠の合計（株＋債券ETF）・残額（運用資金−合計）を円で表示し、合計が運用資金を超える保存（「既定に戻す」・運用資金を下げる保存を含む）は理由を表示して拒否します（監査ログにも残りません）。枠の間で予算を移すときは、減らす方の枠を先に保存します。株の枠は保有中の株の建玉を時価ベース（最新の現在値×株数。現在値が付くまでは取得価格×株数）で数えます。総エクスポージャ上限と同じ計算なので、含み損が出ても%上限より先に枠が埋まることはありません。Paper 専用（Live は変わりません）。 | リスク・資金 |
| 1日の損失上限・利益確保 | `/settings/risk#modal-daily-guard` | 当日の損益・取引回数が上限に達したら新規エントリーを止める条件を編集します。 | リスク・資金 |
| 売買コストの想定 | `/settings/risk#modal-trading_cost` | 手数料・スリッページ・貸株料の想定を編集します（Paper・バックテスト・ラベルが共通で参照します）。 | リスク・資金 |
| 初回セットアップ | `/setup` | 起動に必要な接続（Jev・kabuステーション／立花・Slack など）を順に設定します。必須の接続が未設定の間は他の画面からここへ案内されます。 | 証券会社・データ |
| ブローカー | `/settings/broker#modal-broker` | 市況データと発注の接続先を kabuステーション／立花証券 e支店から選びます。 | 証券会社・データ |
| kabuステーション | `/settings/broker#modal-kabu` | kabuステーション API（時価・発注）の API パスワードを設定します（必須）。 | 証券会社・データ |
| 立花証券 e支店（認証情報） | `/settings/broker#modal-tachibana` | 立花証券 e支店 API の認証ID（デモ・本番）などを設定します。 | 証券会社・データ |
| 立花証券 e支店（接続設定） | `/settings/broker#modal-tachibana-conn` | 立花証券 e支店 API の接続環境・接続先・秘密鍵ファイルを設定します。 | 証券会社・データ |
| 立花 監視銘柄ソース | `/settings/broker#modal-tachibana-source` | 翌営業日の監視銘柄の候補ソースと銘柄リストを編集します。 | 証券会社・データ |
| 立花 夜間日足バッチ | `/settings/broker#modal-tachibana-nightly` | 引け後に全銘柄の日足を取得する夜間バッチの設定を編集します。 | 証券会社・データ |
| 立花 日中の EVENT 接続・REST 時価補助 | `/settings/broker#modal-tachibana-intraday` | 日中の EVENT 接続と REST 時価補助の設定を編集します。 | 証券会社・データ |
| 立花 スクリーニング | `/settings/broker#modal-tachibana-screen` | 日足から監視銘柄を選ぶ指標・重み・上位件数を編集します。 | 証券会社・データ |
| Jev | `/settings/integrations#modal-jev` | 売買判断を行う AI（Jev）の API キー・接続先・モデルを設定します。 | 外部連携・AI |
| Luna | `/settings/integrations#modal-luna` | ニュース分類の AI（Luna）の接続を設定します（既定は Jev）。 | 外部連携・AI |
| Sol | `/settings/integrations#modal-sol` | しきい値の変更案を作る AI（Sol）の接続を設定します（既定は Jev）。 | 外部連携・AI |
| Opus | `/settings/integrations#modal-opus` | Sol の提案を検証する AI（Opus）の接続を設定します（既定は Jev）。 | 外部連携・AI |
| ニュースフィード | `/settings/integrations#modal-news-feed` | ニュースの取得元（既定はやのしん TDnet WebAPI）を設定します。 | 外部連携・AI |
| Slack | `/settings/integrations#modal-slack` | 通知の送信先となる Slack の Incoming Webhook URL を設定します。 | 外部連携・AI |
| MCP | `/settings/integrations#modal-mcp` | Paper 成績を外部の AI クライアントへ公開し、許可した基本設定だけを変更できる MCP サーバーの有効化・API キーの設定と、起動・停止・再起動、MCP からの設定変更の許可（既定オン）と監査ログの確認ができます。 | 外部連携・AI |
| API ドキュメント（Swagger） | `/swagger` | JSON API の仕様を確認します（SWAGGER_ENABLED=true のときだけ表示されます）。 | 外部連携・AI (SWAGGER_ENABLED=true のときのみ) |
| アップデート | `/settings/maintenance#modal-update` | 現在のバージョンと最終確認結果を確認し、新しいバージョンを確認します。 | システム |
| エラーログ | `/settings/maintenance#modal-error-log` | 調査・共有のためにエラーログをダウンロードします。 | システム |
| データエクスポート | `/settings/maintenance#modal-data-export` | 取引ログ・判断ログ・計測ログを期間を指定して ZIP でダウンロードします。 | システム |
| バックアップ先 | `/settings/maintenance#modal-backup` | データベースの日次バックアップ・週次アーカイブの保存先ディレクトリを設定します。 | システム |
| ログディレクトリ | `/settings/maintenance#modal-logdir` | 日次 JSON ログとエラーログの保存先ディレクトリを設定します。 | システム |
| ログのレベルと保持期間 | `/settings/maintenance#modal-logsettings` | ログの最低レベルと、圧縮せずに置いておく日数を設定します。 | システム |

## 3. 保守のルール

- Settings にカード（ダイアログ）を足す・消す・ID を変えるときは、`internal/web/settingspages/registry.go` の `dialogs`（どのページに置くか）も同時に変える。新しいページは `pages` に1行足し、そのページのダイアログ・リンクを `dialogs`・`links` に足す（ルートは `internal/router/router_routes.go` が `Pages()` から登録するので足さない）。`internal/router/settingsroutes` のテストが、各ページが描画するダイアログと登録簿の一致（漏れ・重複なし）、全運用設定キー（`opsettings.Keys()`）がちょうど1ページで編集できること、メインメニューの各画面と設定の各ページが2クリック以内で開けること、各ページで正しいナビ項目に `aria-current` が付くこと、リンクのアンカーが実在することを検査する。`internal/web/handler/settings/pageflow` のテストが、全接続先・全設定グループが登録簿のちょうど1ページに置かれることを検査する。
- メインメニューに載る画面は Settings の `links` に重ねて置かない（`TestLinks_DoNotRepeatTheMainMenu`）。メインメニューにない画面（稼働ログ・初回セットアップ・Swagger）は `links` に置き、設定のページ一覧にも出す（`settingspages.HubLinks()`）。
- 既存 URL は変えない。`internal/router/settingsroutes` の URL 生存テストが、既存の GET ルートの登録と非 404（`/admin`・`/settings/asset-buckets` は 301）を検査する。
- Settings に載せないもの: パラメータ付きの Symbol Detail（候補銘柄の候補から開く）、アクション用ルート（`POST`/`DELETE`）、API・WebSocket。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-19 | 新規作成（管理ハブ `GET /admin` のナビ構造・画面一覧・保守ルール） | issue #793 |
| 1.1 | 2026-10-19 | 管理ハブの MCP の説明に、起動・停止・再起動ができることを追記（画面は Settings の MCP カードのまま） | issue #795 |
| 1.2 | 2026-10-20 | 管理ハブ（`/admin`）を Settings に統合: メニューは Settings 1本、`/settings` はページ一覧、詳細は `/settings/<page>`（運用／成績・検証／リスク・資金／ブローカー・データ／外部連携／ログ・エクスポート）。`/admin` と旧 `/settings#modal-<id>` は新ページへ転送。登録簿を `adminhub` から `settingspages` へ | issue #805 |
| 1.3 | 2026-10-20 | Settings「リスク・資金」ページに「Paper の運用資金・建玉上限・1回の最大損失」カード（`modal-paper-risk`）を追加し、ページ説明を更新 | issue #804 |
| 1.4 | 2026-10-20 | MCP カードの説明に、MCP からの設定変更の許可（既定オン）と監査ログを追記 | issue #806 |
| 1.5 | 2026-10-20 | Settings「成績・検証」ページの「メタラベリング」カードの説明に、LLM cutoff の状態表示（仮置き／未確認／確認済み）と公式日付の確認手順のヘルプを追記 | issue #809 |
| 1.6 | 2026-10-20 | Settings に7つ目のページ「資産ごとの予算枠」（`/settings/asset-buckets`、カード `modal-asset-buckets`。株・債券ETFの予算枠の2キーと、合計・残額の「計算結果」）を追加し、一覧・ページ表を更新。「リスク・資金」ページの Paper 運用資金カードの説明に、枠の合計より下げる保存は拒否される旨を追記 | issue #824 |
| 1.7 | 2026-10-21 | メインナビと画面一覧に「注文案」（`GET /order-proposals`・`GET /order-proposals/:id`。読み取り専用）を追加。Settings「運用」ページのリンクにも載せる | issue #829 |
| 1.8 | 2026-10-22 | 注文案の詳細のトレードシグナルに、Risk を通ったが後段で拒否された案を「Risk check: 通過」「後段で拒否: <コード>」と示す表示を追加（Risk Engine 自身の拒否は従来どおり） | issue #860 |
| 1.9 | 2026-10-21 | Settings「運用」ページに「スリープ防止」（`/settings/operations#modal-sleep-guard`）を追加。全ページ共通の警告バナーに、kabu ステーションに接続できない（未起動）場合の文言を追加 | issue #863 |
| 1.10 | 2026-10-23 | メインメニューを「ホーム／候補銘柄／AIの注文提案／成績／（区切り）／設定」に整理（ラベルを日本語化、`NavHome` を追加、監視リスト・銘柄詳細は候補銘柄、予測の精度は成績、稼働ログは設定を強調）。候補銘柄・成績にタブを追加、`/performance#backtest` は `?tab=backtest` へ転送。Settings を6ページ（取引ルール／リスク・資金／予測モデル／証券会社・データ／外部連携・AI／システム）に再編し、予算枠はリスク・資金へ統合（`/settings/asset-buckets` は 301）、スリープ防止はシステムへ移動、メインメニューと重複する「関連画面」を削除して稼働ログのリンクを追加。「注文案」の表示名を「AIの注文提案」へ変更 | issue #895 |
| 1.11 | 2026-10-23 | ホーム画面（`/`）を新設（取引の状態・今日の Paper 損益・保有ポジション・要対応・今日の詳細。`/scanner` へのリダイレクトを廃止し、ロゴのリンク先と「ホーム」の強調を `/` に）。ヘッダーに積んでいたバナーを「要対応 N 件」バッジに縮約。Kill Switch パネルはホームでは取引の状態カード内 | issue #896（#899 の上に積む） |
| 1.12 | 2026-10-23 | ホームの保有ポジション・今日の損益が`closed_at`の無い全行を数えるよう修正、損失上限に達したら「損失上限に達しました」と表示、見送り理由はエントリー品質が最多と同数でも最低エントリー品質を併記 | issue #896（#899の上に積む。レビュー修正） |
