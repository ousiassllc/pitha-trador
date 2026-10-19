# 画面一覧と管理ハブ（issue #793）

`docs/components/overview.md` §3 の分割章。管理系の画面・機能の一覧と、それらの入口である管理ハブ（`GET /admin`）の構成を定める。登録簿は `internal/web/adminhub`（プレーンな Go データ。ハブの描画とテストが同じ登録簿を読む）。

## 1. ナビゲーション構造

- メインナビ（`organisms.MainNav`）は左から Scanner / Watchlist / Performance / Calibration / Activity / **管理（`/admin`）** / Settings。`organisms.NavAdmin` が管理を表し、`layout.Shell("管理", organisms.NavAdmin)` を使う `AdminPage` だけが `aria-current="page"` を付ける。
- 管理ハブ（`pages.AdminPage` → `organisms.AdminHub`、ハンドラは `internal/web/handler/admin`）は、`adminhub.Visible(swagger)` のカテゴリ順に、カテゴリ見出し（`<h2>`）・カテゴリの要約・項目（名前のリンク・できること・「開く」ボタン）を並べる。リンク先が `/settings#<ダイアログ id>` の項目は、`pitha-modal` がハッシュの指すダイアログを開く。
- **戻り導線の規則**: `layout.Shell` を使う全ページはヘッダーのメインナビに「管理」を持つため、どの管理画面からも1操作でハブへ戻れる。例外は `/setup`（`SetupShell`。ヘッダーを持たない初回専用画面）と `/swagger`（静的なドキュメント画面）。Settings のダイアログを開いたままでもヘッダーは隠れない。
- `/admin` は他のページと同じく Setup Guard の対象（`/setup`・`/static`・秘密キーの `POST`/`DELETE /settings/:key` のみが例外）。
- カテゴリ（この名前・順序で固定。`adminhub.Categories()`）: 運用（スキャン・ポジション・Kill Switch）／成績・検証（Performance・Calibration・バックテスト）／リスク・資金設定／ブローカー・データ接続（kabu・立花）／外部連携（Jev・Luna・Sol・Opus・ニュースフィード・Slack・MCP）／ログ・エクスポート（アップデート・バックアップ・ログ設定を含む保守）。

## 2. 画面一覧

管理系の全画面・機能。`/settings#…` の行は Settings 画面のカード（`molecules.SettingsCard`）が開くダイアログ（`molecules.Modal`）で、ID は `modal-<カードの id>`。立花の接続設定ダイアログ（`modal-tachibana-conn`）は、接続先カードの `modal-tachibana`（認証情報）と ID が重ならないよう運用設定グループ側の id を `tachibana-conn` としている。

| 画面名 | URL | できること | カテゴリ |
|--------|-----|-----------|----------|
| Scanner | `/scanner` | スキャンの絞り込み状況と候補銘柄・Jev の判断を確認します（候補から銘柄詳細へ進めます）。 | 運用 |
| Watchlist | `/watchlist` | 立花の監視リスト（どの銘柄をどう選んだか）を確認します。 | 運用 |
| Activity | `/activity` | システムの稼働ログとキューの状況を確認します。 | 運用 |
| Settings | `/settings` | 接続先・運用設定・システム（アップデート・ログ）を一つの画面にまとめた設定画面です。個々の項目は下の各カテゴリから直接開けます。 | 運用 |
| Kill Switch | `/scanner#kill-switch` | 全ページのヘッダー右上にあるパネルで、新規エントリーの一時停止・再開と全決済（Kill Switch）を操作します。専用の URL はないため、ヘッダーのあるページ（Scanner）を開きます。 | 運用 |
| Fast Screener | `/settings#modal-screener` | 候補を絞り込むしきい値・上位件数・スコア重みを編集します。 | 運用 |
| Policy Engine（ロング） | `/settings#modal-policy-long` | ロングのシグナルになるための確率・エントリー品質などのしきい値を編集します。 | 運用 |
| Policy Engine（ショート） | `/settings#modal-policy-short` | ショートのシグナルになるための確率・エントリー品質などのしきい値を編集します。 | 運用 |
| データの鮮度（ランキング監視） | `/settings#modal-snapshot-age` | Jev の判断と Paper エントリーが最新の足をどこまで古くてよしとするかの上限（秒）を編集します。 | 運用 |
| Performance | `/performance` | 決済済みポジションの実績（Paper）と評価指標を確認します。 | 成績・検証 |
| バックテスト（Walk Forward） | `/performance#backtest` | 期間を指定して Walk Forward バックテストを実行し、コスト感度を確認します（Performance 画面の下部）。 | 成績・検証 |
| Calibration | `/calibration` | Jev の確率予測が実際の結果とどれだけ合っているかをヒートマップで確認します。 | 成績・検証 |
| キャリブレーションのラベル（トリプルバリア） | `/settings#modal-triple_barrier` | 判断に正解・不正解のラベルを付ける利確・損切り幅と時間切れを編集します。 | 成績・検証 |
| メタラベリング | `/settings#modal-metalabel` | 入る／見送るを決める二次モデルの設定と最低 EV を編集します。 | 成績・検証 |
| 相場状態（レジーム）判定 | `/settings#modal-regime` | 相場状態（トレンド／平均回帰）の判定と、VWAP 回帰モードへの切替を編集します。 | 成績・検証 |
| 板の需給（OFI） | `/settings#modal-orderflow` | 板の需給（Order Flow Imbalance）を集計する窓の幅を編集します。 | 成績・検証 |
| 1日の損失上限・利益確保 | `/settings#modal-daily-guard` | 当日の損益・取引回数が上限に達したら新規エントリーを止める条件を編集します。 | リスク・資金設定 |
| 売買コストの想定 | `/settings#modal-trading_cost` | 手数料・スリッページ・貸株料の想定を編集します（Paper・バックテスト・ラベルが共通で参照します）。 | リスク・資金設定 |
| 初回セットアップ | `/setup` | 起動に必要な接続（Jev・kabuステーション／立花・Slack など）を順に設定します。必須の接続が未設定の間は他の画面からここへ案内されます。 | ブローカー・データ接続 |
| ブローカー | `/settings#modal-broker` | 市況データと発注の接続先を kabuステーション／立花証券 e支店から選びます。 | ブローカー・データ接続 |
| kabuステーション | `/settings#modal-kabu` | kabuステーション API（時価・発注）の API パスワードを設定します（必須）。 | ブローカー・データ接続 |
| 立花証券 e支店（認証情報） | `/settings#modal-tachibana` | 立花証券 e支店 API の認証ID（デモ・本番）などを設定します。 | ブローカー・データ接続 |
| 立花証券 e支店（接続設定） | `/settings#modal-tachibana-conn` | 立花証券 e支店 API の接続環境・接続先・秘密鍵ファイルを設定します。 | ブローカー・データ接続 |
| 立花 監視銘柄ソース | `/settings#modal-tachibana-source` | 翌営業日の監視銘柄の候補ソースと銘柄リストを編集します。 | ブローカー・データ接続 |
| 立花 夜間日足バッチ | `/settings#modal-tachibana-nightly` | 引け後に全銘柄の日足を取得する夜間バッチの設定を編集します。 | ブローカー・データ接続 |
| 立花 日中の EVENT 接続・REST 時価補助 | `/settings#modal-tachibana-intraday` | 日中の EVENT 接続と REST 時価補助の設定を編集します。 | ブローカー・データ接続 |
| 立花 スクリーニング | `/settings#modal-tachibana-screen` | 日足から監視銘柄を選ぶ指標・重み・上位件数を編集します。 | ブローカー・データ接続 |
| Jev | `/settings#modal-jev` | 売買判断を行う AI（Jev）の API キー・接続先・モデルを設定します。 | 外部連携 |
| Luna | `/settings#modal-luna` | ニュース分類の AI（Luna）の接続を設定します（既定は Jev）。 | 外部連携 |
| Sol | `/settings#modal-sol` | しきい値の変更案を作る AI（Sol）の接続を設定します（既定は Jev）。 | 外部連携 |
| Opus | `/settings#modal-opus` | Sol の提案を検証する AI（Opus）の接続を設定します（既定は Jev）。 | 外部連携 |
| ニュースフィード | `/settings#modal-news-feed` | ニュースの取得元（既定はやのしん TDnet WebAPI）を設定します。 | 外部連携 |
| Slack | `/settings#modal-slack` | 通知の送信先となる Slack の Incoming Webhook URL を設定します。 | 外部連携 |
| MCP | `/settings#modal-mcp` | Paper 成績を読み取り専用で外部の AI クライアントへ公開する MCP サーバーの有効化・API キーを設定します。 | 外部連携 |
| API ドキュメント（Swagger） | `/swagger` | JSON API の仕様を確認します（SWAGGER_ENABLED=true のときだけ表示されます）。 | 外部連携 (SWAGGER_ENABLED=true のときのみ) |
| アップデート | `/settings#modal-update` | 現在のバージョンと最終確認結果を確認し、新しいバージョンを確認します。 | ログ・エクスポート |
| エラーログ | `/settings#modal-error-log` | 調査・共有のためにエラーログをダウンロードします。 | ログ・エクスポート |
| データエクスポート | `/settings#modal-data-export` | 取引ログ・判断ログ・計測ログを期間を指定して ZIP でダウンロードします。 | ログ・エクスポート |
| バックアップ先 | `/settings#modal-backup` | データベースの日次バックアップ・週次アーカイブの保存先ディレクトリを設定します。 | ログ・エクスポート |
| ログディレクトリ | `/settings#modal-logdir` | 日次 JSON ログとエラーログの保存先ディレクトリを設定します。 | ログ・エクスポート |
| ログのレベルと保持期間 | `/settings#modal-logsettings` | ログの最低レベルと、圧縮せずに置いておく日数を設定します。 | ログ・エクスポート |
| Symbol Detail | `/symbols/:symbol` | 銘柄のチャート・Jev の判断・Risk・保有ポジション（決済）を確認します。Scanner の候補から遷移するパラメータ付きの画面で、ハブにはリンクを置きません。ナビは Scanner を強調します | 運用（ハブ対象外） |
| 管理（本ハブ） | `/admin` | 上記すべての入口をカテゴリ別に一覧します | — |

## 3. 保守のルール

- Settings にカード（ダイアログ）を足す・消す・ID を変えるときは、`internal/web/adminhub/registry.go` の項目も同時に変える。`internal/router/adminroutes` のテストが、描画した Settings の全ダイアログがハブから開けること（漏れなし）と、ハブの `/settings#<id>` がすべて実在するダイアログを指すことを検査する。
- ナビ項目を足すときはハブにも載せる（同テストが検査する）。
- 既存 URL は変えない。`internal/router/adminroutes` の URL 生存テストが、既存の GET ルートの登録と非 404 を検査する。
- ハブに載せないもの: パラメータ付きの Symbol Detail（Scanner の候補から開く）、アクション用ルート（`POST`/`DELETE`）、API・WebSocket。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-19 | 新規作成（管理ハブ `GET /admin` のナビ構造・画面一覧・保守ルール） | issue #793 |
