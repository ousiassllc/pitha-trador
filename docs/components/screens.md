# 画面一覧と Settings の構成（issue #793・#805）

`docs/components/overview.md` §3 の分割章。管理系の画面・機能の一覧と、それらの入口である Settings（`GET /settings`）の構成を定める。旧「管理ハブ」（`GET /admin`、issue #793）は issue #805 で Settings に統合した。登録簿は `internal/web/settingspages`（プレーンな Go データ。Settings の描画とテストが同じ登録簿を読む）。

## 1. ナビゲーション構造

- メインナビ（`organisms.MainNav`）は左から Scanner / Watchlist / Performance / Calibration / Activity / **Settings（`/settings`）**。管理系の入口は Settings 1本で、単独の「管理」項目は置かない。`organisms.NavSettings` が Settings を表し、`/settings` と、その下のすべてのページが `layout.Shell(…, organisms.NavSettings)` を使うので、どのページでも Settings だけに `aria-current="page"` が付く。
- **Settings の階層**: `/settings`（ページ一覧。`pages.SettingsHubPage`）→ `/settings/<page id>`（詳細設定のページ。`pages.SettingsPage`）→ 各カードのダイアログ（`molecules.Modal`）。一覧の各行（`molecules.SettingsLinkCard`）は、ページ名・要約・「開く」リンクを持つ。詳細ページは、パンくず（Settings / ページ名）・そのページに属する接続先カード・設定項目カード・システムカード・関連画面へのリンク（`settingspages.LinksOf`）を並べる。カードの「開く」「設定する」ボタンでダイアログが開き、ダイアログの中で項目ごとに保存・削除する。
- ページ（この名前・順序で固定。`settingspages.Pages()`）: 運用 `/settings/operations`（スキャン・ポジション・Kill Switch、候補を絞るしきい値）／成績・検証 `/settings/results`（Performance・Calibration・バックテスト、判断のラベル・相場状態）／リスク・資金 `/settings/risk`（Paper の運用資金・建玉上限、損失上限・利益確保・売買コストの想定）／資産ごとの予算枠 `/settings/asset-buckets`（Paper の運用資金を株・債券ETFの枠に分けた予算（円）と、合計・残額）／ブローカー・データ `/settings/broker`（kabu・立花の接続と監視銘柄）／外部連携 `/settings/integrations`（Jev・Luna・Sol・Opus・ニュースフィード・Slack・MCP）／ログ・エクスポート `/settings/maintenance`（アップデート・エラーログ・データエクスポート・バックアップ・ログ設定）。
- **戻り導線の規則**: `layout.Shell` を使う全ページはヘッダーのメインナビに「Settings」を持つため、どの画面からも1操作で Settings のページ一覧へ戻れる。詳細ページはパンくずからも一覧へ戻れる。例外は `/setup`（`SetupShell`。ヘッダーを持たない初回専用画面）と `/swagger`（静的なドキュメント画面）。ダイアログを開いたままでもヘッダーは隠れない。
- **旧 URL**: `/admin` は `/settings` へ 301 でリダイレクトする。旧い `/settings#modal-<id>`（ハッシュ付きの Settings 直リンク）と `/settings#update-panel`（ヘッダーのバージョン表示の旧リンク先）は、サーバーにはハッシュが届かないため、一覧ページが埋め込む対応表（`data-hash-routes`、`settingspages.HashRoutes()`）を `pitha-settings-hub` が読み、該当ページ `/settings/<page id>#<id>` へ `location.replace` で転送する。ヘッダーのバージョン表示の現在のリンク先は `/settings/maintenance#update-panel`。
- Settings の一覧・各ページは他のページと同じく Setup Guard の対象（`/setup`・`/static`・秘密キーの `POST`/`DELETE /settings/:key` のみが例外）。

## 2. 画面一覧

管理系の全画面・機能。`/settings/<page>#…` の行は、そのページのカード（`molecules.SettingsCard`）が開くダイアログ（`molecules.Modal`）で、ID は `modal-<カードの id>`。立花の接続設定ダイアログ（`modal-tachibana-conn`）は、接続先カードの `modal-tachibana`（認証情報）と ID が重ならないよう運用設定グループ側の id を `tachibana-conn` としている。

| 画面名 | URL | できること | Settings のページ |
|--------|-----|-----------|------------------|
| Scanner | `/scanner` | スキャンの絞り込み状況と候補銘柄・Jev の判断を確認します（候補から銘柄詳細へ進めます）。 | 運用 |
| Watchlist | `/watchlist` | 立花の監視リスト（どの銘柄をどう選んだか）を確認します。 | 運用 |
| Activity | `/activity` | システムの稼働ログとキューの状況を確認します。 | 運用 |
| Settings | `/settings` | 設定のページ一覧です。7つのページ（運用／成績・検証／リスク・資金／資産ごとの予算枠／ブローカー・データ／外部連携／ログ・エクスポート）から開きたい設定を選びます。旧 `/admin`（管理ハブ）は `/settings` へ恒久リダイレクトします。 | — |
| Kill Switch | `/scanner#kill-switch` | 全ページのヘッダー右上にあるパネルで、新規エントリーの一時停止・再開と全決済（Kill Switch）を操作します。専用の URL はないため、ヘッダーのあるページ（Scanner）を開きます。 | 運用 |
| Fast Screener | `/settings/operations#modal-screener` | 候補を絞り込むしきい値・上位件数・スコア重みを編集します。 | 運用 |
| Policy Engine（ロング） | `/settings/operations#modal-policy-long` | ロングのシグナルになるための確率・エントリー品質などのしきい値を編集します。 | 運用 |
| Policy Engine（ショート） | `/settings/operations#modal-policy-short` | ショートのシグナルになるための確率・エントリー品質などのしきい値を編集します。 | 運用 |
| データの鮮度（ランキング監視） | `/settings/operations#modal-snapshot-age` | Jev の判断と Paper エントリーが最新の足をどこまで古くてよしとするかの上限（秒）を編集します。 | 運用 |
| Paper ブートストラップ | `/settings/operations#modal-paper-bootstrap` | メタモデル未採用の間だけ Paper で旧ルールのエントリーを許し、ラベルを貯めるスイッチ（既定は無効、Live では必ず無効）を編集します。 | 運用 |
| Performance | `/performance` | 決済済みポジションの実績（Paper）と評価指標を確認します。 | 成績・検証 |
| バックテスト（Walk Forward） | `/performance#backtest` | 期間を指定して Walk Forward バックテストを実行し、コスト感度を確認します（Performance 画面の下部）。 | 成績・検証 |
| Calibration | `/calibration` | Jev の確率予測が実際の結果とどれだけ合っているかをヒートマップで確認します。 | 成績・検証 |
| キャリブレーションのラベル（トリプルバリア） | `/settings/results#modal-triple_barrier` | 判断に正解・不正解のラベルを付ける利確・損切り幅と時間切れを編集します。 | 成績・検証 |
| メタラベリング | `/settings/results#modal-metalabel` | 入る／見送るを決める二次モデルの設定と最低 EV を編集します。上部に LLM cutoff の状態（仮置き／未確認／確認済み）と、公式の日付の確認手順（どこを見て何を入れるか）を表示します。 | 成績・検証 |
| 相場状態（レジーム）判定 | `/settings/results#modal-regime` | 相場状態（トレンド／平均回帰）の判定と、VWAP 回帰モードへの切替を編集します。 | 成績・検証 |
| 板の需給（OFI） | `/settings/results#modal-orderflow` | 板の需給（Order Flow Imbalance）を集計する窓の幅を編集します。 | 成績・検証 |
| Paper の運用資金・建玉上限・1回の最大損失 | `/settings/risk#modal-paper-risk` | Paper の運用資金（ポジションサイズの分母）・1銘柄の上限・総エクスポージャの上限・1回の取引の最大損失（%）と、日次ガード用の運用資金を並べて編集します。保存は再起動不要で次のリスク判定から反映され、1単元（100株）が運用資金を超える場合は注意を表示します。運用資金を、株・債券ETFの枠の合計より下げる保存は拒否されます。Paper 専用（Live は変わりません）。 | リスク・資金 |
| 資産ごとの予算枠 | `/settings/asset-buckets#modal-asset-buckets` | Paper の運用資金を分ける、株の枠と債券ETFの枠（円。0 はその枠を使わない）を編集します。「計算結果」に運用資金・枠の合計（株＋債券ETF）・残額（運用資金−合計）を円で表示し、合計が運用資金を超える保存（「既定に戻す」・運用資金を下げる保存を含む）は理由を表示して拒否します（監査ログにも残りません）。枠の間で予算を移すときは、減らす方の枠を先に保存します。株の枠は保有中の株の建玉を時価ベース（最新の現在値×株数。現在値が付くまでは取得価格×株数）で数えます。総エクスポージャ上限と同じ計算なので、含み損が出ても%上限より先に枠が埋まることはありません。Paper 専用（Live は変わりません）。 | 資産ごとの予算枠 |
| 1日の損失上限・利益確保 | `/settings/risk#modal-daily-guard` | 当日の損益・取引回数が上限に達したら新規エントリーを止める条件を編集します。 | リスク・資金 |
| 売買コストの想定 | `/settings/risk#modal-trading_cost` | 手数料・スリッページ・貸株料の想定を編集します（Paper・バックテスト・ラベルが共通で参照します）。 | リスク・資金 |
| 初回セットアップ | `/setup` | 起動に必要な接続（Jev・kabuステーション／立花・Slack など）を順に設定します。必須の接続が未設定の間は他の画面からここへ案内されます。 | ブローカー・データ |
| ブローカー | `/settings/broker#modal-broker` | 市況データと発注の接続先を kabuステーション／立花証券 e支店から選びます。 | ブローカー・データ |
| kabuステーション | `/settings/broker#modal-kabu` | kabuステーション API（時価・発注）の API パスワードを設定します（必須）。 | ブローカー・データ |
| 立花証券 e支店（認証情報） | `/settings/broker#modal-tachibana` | 立花証券 e支店 API の認証ID（デモ・本番）などを設定します。 | ブローカー・データ |
| 立花証券 e支店（接続設定） | `/settings/broker#modal-tachibana-conn` | 立花証券 e支店 API の接続環境・接続先・秘密鍵ファイルを設定します。 | ブローカー・データ |
| 立花 監視銘柄ソース | `/settings/broker#modal-tachibana-source` | 翌営業日の監視銘柄の候補ソースと銘柄リストを編集します。 | ブローカー・データ |
| 立花 夜間日足バッチ | `/settings/broker#modal-tachibana-nightly` | 引け後に全銘柄の日足を取得する夜間バッチの設定を編集します。 | ブローカー・データ |
| 立花 日中の EVENT 接続・REST 時価補助 | `/settings/broker#modal-tachibana-intraday` | 日中の EVENT 接続と REST 時価補助の設定を編集します。 | ブローカー・データ |
| 立花 スクリーニング | `/settings/broker#modal-tachibana-screen` | 日足から監視銘柄を選ぶ指標・重み・上位件数を編集します。 | ブローカー・データ |
| Jev | `/settings/integrations#modal-jev` | 売買判断を行う AI（Jev）の API キー・接続先・モデルを設定します。 | 外部連携 |
| Luna | `/settings/integrations#modal-luna` | ニュース分類の AI（Luna）の接続を設定します（既定は Jev）。 | 外部連携 |
| Sol | `/settings/integrations#modal-sol` | しきい値の変更案を作る AI（Sol）の接続を設定します（既定は Jev）。 | 外部連携 |
| Opus | `/settings/integrations#modal-opus` | Sol の提案を検証する AI（Opus）の接続を設定します（既定は Jev）。 | 外部連携 |
| ニュースフィード | `/settings/integrations#modal-news-feed` | ニュースの取得元（既定はやのしん TDnet WebAPI）を設定します。 | 外部連携 |
| Slack | `/settings/integrations#modal-slack` | 通知の送信先となる Slack の Incoming Webhook URL を設定します。 | 外部連携 |
| MCP | `/settings/integrations#modal-mcp` | Paper 成績を外部の AI クライアントへ公開し、許可した基本設定だけを変更できる MCP サーバーの有効化・API キーの設定と、起動・停止・再起動、MCP からの設定変更の許可（既定オン）と監査ログの確認ができます。 | 外部連携 |
| API ドキュメント（Swagger） | `/swagger` | JSON API の仕様を確認します（SWAGGER_ENABLED=true のときだけ表示されます）。 | 外部連携 (SWAGGER_ENABLED=true のときのみ) |
| アップデート | `/settings/maintenance#modal-update` | 現在のバージョンと最終確認結果を確認し、新しいバージョンを確認します。 | ログ・エクスポート |
| エラーログ | `/settings/maintenance#modal-error-log` | 調査・共有のためにエラーログをダウンロードします。 | ログ・エクスポート |
| データエクスポート | `/settings/maintenance#modal-data-export` | 取引ログ・判断ログ・計測ログを期間を指定して ZIP でダウンロードします。 | ログ・エクスポート |
| バックアップ先 | `/settings/maintenance#modal-backup` | データベースの日次バックアップ・週次アーカイブの保存先ディレクトリを設定します。 | ログ・エクスポート |
| ログディレクトリ | `/settings/maintenance#modal-logdir` | 日次 JSON ログとエラーログの保存先ディレクトリを設定します。 | ログ・エクスポート |
| ログのレベルと保持期間 | `/settings/maintenance#modal-logsettings` | ログの最低レベルと、圧縮せずに置いておく日数を設定します。 | ログ・エクスポート |
| Symbol Detail | `/symbols/:symbol` | 銘柄のチャート・Jev の判断・Risk・保有ポジション（決済）を確認します。Scanner の候補から遷移するパラメータ付きの画面で、Settings のページにはリンクを置きません。ナビは Scanner を強調します | 運用（Settings のページ対象外） |

## 3. 保守のルール

- Settings にカード（ダイアログ）を足す・消す・ID を変えるときは、`internal/web/settingspages/registry.go` の `dialogs`（どのページに置くか）も同時に変える。新しいページは `pages` に1行足し、そのページのダイアログ・リンクを `dialogs`・`links` に足す（ルートは `internal/router/router_routes.go` が `Pages()` から登録するので足さない）。`internal/router/settingsroutes` のテストが、各ページが描画するダイアログと登録簿の一致（漏れ・重複なし）、全運用設定キー（`opsettings.Keys()`）がちょうど1ページで編集できること、メインナビの各項目へ Settings のリンクから辿れること、リンクのアンカーが実在することを検査する。`internal/web/handler/settings/pageflow` のテストが、全接続先・全設定グループが登録簿のちょうど1ページに置かれることを検査する。
- ナビ項目を足すときは、いずれかのページの `links` にも載せる（同テストが検査する）。
- 既存 URL は変えない。`internal/router/settingsroutes` の URL 生存テストが、既存の GET ルートの登録と非 404（`/admin` は 301）を検査する。
- Settings に載せないもの: パラメータ付きの Symbol Detail（Scanner の候補から開く）、アクション用ルート（`POST`/`DELETE`）、API・WebSocket。

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
