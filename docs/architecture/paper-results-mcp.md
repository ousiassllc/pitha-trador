# 読み取り専用 Paper 実績 MCP（Grok Bot コネクタ）

リモートの Grok Bot が、pitha-trador の SQLite に入っている Paper の取引実績・Jev 判断・メタモデル・Risk・収集状況の**集計とラベル**を読む仕様。issue #783。実装・実行ファイル・テストコードは本仕様の範囲外であり、この変更ではリポジトリに置かない。

ツールの入出力・上限・ホワイトリストは `docs/api/endpoints/paper-results-mcp.md`。エラー表は `docs/architecture/paper-results-mcp-errors.md`。置き場所とプロセス境界は `docs/components/paper-results-mcp.md`。機能要件は FR-MCP-1〜8（`docs/requirements/functional/components-platform.md` §4.22）。

## 1. 目的

実績の DB は Windows PC の `%AppData%\pitha-trador\pitha.db`（`internal/bootstrap/bootstrap.go` の `DefaultDBPath`。環境変数 `PITHA_DB_PATH` があればそれを優先）。Grok Bot はその PC では動かない。いまは DB ファイルを手渡しして集計している。本 MCP は、その手渡しの代わりに、認証を通した読み取りだけをトンネル越しに届ける。

```mermaid
flowchart LR
    Grok["Grok Bot（リモート）"]
    Access["Cloudflare Access\n（サービストークン）"]
    Tunnel["Cloudflare Tunnel\n（同一 PC の cloudflared）"]
    MCP["Paper 実績 MCP\n127.0.0.1:48110"]
    DB[("SQLite\nmode=ro")]
    Grok -->|"HTTPS と認証ヘッダ"| Access
    Access --> Tunnel
    Tunnel -->|"loopback のみ"| MCP
    MCP -->|"認証通過後だけ"| DB
```

MCP はアプリ本体（Wails / `cmd/server`）とは別プロセスで、同じ Windows ユーザーのセッションで動く。話しかたは HTTP である（stdio ではない）。待受は loopback だけにし、インターネットへ直接は公開しない。外から届く道は Cloudflare Tunnel だけである（悠太さん決定、2026-10-17。公開の入口をトンネルに限る）。

## 2. issue #347 との関係

#347（クローズ済み。仕様はブランチ `cursor/kabu-status-mcp-spec-af46` の `docs/architecture/kabu-status-mcp.md` と `kabu-status-mcp-errors.md`。main には未マージ）は、**kabuステーションAPI の待受・ログイン・API 利用設定・パスワード不一致を読む** MCP である。本仕様はそれを踏襲するが、読む対象は別である。

| 項目 | #347 kabu ステータス MCP | 本仕様（#783） |
|------|--------------------------|----------------|
| 読むもの | 同一 PC の kabu `POST /token` と、`secrets` の `KABU_API_PASSWORD` | アプリの SQLite の集計・ラベル。`secrets` は読まない |
| トンネル | プライベートトンネル（Tailscale 等）。インターネット非公開 | Cloudflare Tunnel。待受自体は loopback |
| 認証 | コネクタ用の別 API キー。無い・不一致は kabu へ繋ぐ前に拒否 | 推奨は二重（§3.3）。キー不一致は SQL を発行する前に拒否 |
| 既定 | オフ。PC 上のローカル設定だけ。Grok からは戻せない | 同じ。ただしオフのとき待受しない（下の差分） |
| オフの応答 | キー一致のあと `issue=mcp_disabled` を返す | TCP 待受を開かない（接続不可）。#783 の受け入れ条件 |
| ポート | MCP が kabu の localhost:18080（検証時 18081）を叩く | 本 MCP の待受は `127.0.0.1:48110`。kabu のポートは使わない |
| プロセスと鍵 | アプリの HTTP を叩かない。鍵は `KABU_API_PASSWORD` とは別 | 別プロセス・別の設定ファイル・別の API キー。#347 のツールは呼ばない |

衝突させない。#347 のスイッチやコネクタ鍵を、本 MCP のオン/オフや鍵に流用しない。kabu の待受やログイン状態は本 MCP のツールに出さない。#347 の仕様文は本仕様で書き換えない（main 未マージの別文書のまま）。

## 3. 接続と認証

### 3.1 待受

- 既定はオフ。オフの間はポートを bind しない。プロセスが起動していても接続は拒否される（`paper-results-mcp-errors.md` の `not_listening`）
- オンは Windows PC のローカル設定ファイルだけが行う（§3.4）。Grok Bot のツール引数、コネクタ、DB の `runtime_settings` からはオンに戻せない
- オンのとき bind するのは `127.0.0.1:48110` だけである。`0.0.0.0`・LAN・非 loopback は起動を拒否する（`cmd/server` の `PITHA_SERVER_ALLOW_NON_LOOPBACK` に相当する抜け道は置かない）。根拠: 外への出口はトンネルに一本化し、アプリの待受（`127.0.0.1:48080`）や kabu（18080/18081）とポートを分けて衝突を避ける
- `48110` が使用中なら、別インターフェースへ逃げずに起動失敗とする
- Grok Bot に登録する URL はトンネルの HTTPS ホスト名である。`http://127.0.0.1:48110` も kabu の URL も登録しない

### 3.2 Cloudflare Tunnel と Access

同一 PC の `cloudflared` が、トンネルの ingress を `http://127.0.0.1:48110` へ向ける。ポート転送で待受を LAN やインターネットへ出さない。

Cloudflare Access はトンネルの入口でサービストークンを要求する第一因子である。クライアントが提示するヘッダは `CF-Access-Client-Id` と `CF-Access-Client-Secret`（URL・クエリ・パスには置かない）。Access のポリシー自体は Cloudflare 側の設定であり、本リポジトリにはアカウント ID・トークン・ホスト名の実値を書かない。

**要確認**: `cloudflared` がオリジン（本 MCP）までこの 2 ヘッダを転送するか。転送されない構成でオリジンがヘッダ照合を必須にすると、Access を通過した正規クライアントも拒否される。実装の最初の確認項目にし、転送の有無をテストで固定する。転送されないと分かった場合の読み替えは §3.3 の「Access 単独」とする（エッジの Access が第一因子、オリジンは API キーのみ）。

### 3.3 二重認証

推奨は二重である。根拠: 入口（Access）とアプリ（API キー）を分けると、片方の設定漏れだけでは DB に届かない。

| モード | 成立するか | 条件 |
|--------|------------|------|
| 二重（推奨） | 成立する | エッジの Access サービストークンに加え、MCP が `Authorization: Bearer` の API キーを検証する。ローカル設定 `verify_access_headers: true` かつ `api_key` が非空。ヘッダ転送が確認できた場合、オリジンは `CF-Access-Client-Id` / `CF-Access-Client-Secret` もローカル設定と定数時間で比較する |
| API キーのみ | 成立する | `verify_access_headers: false` かつ `api_key` が非空。トンネルに Access が無くてもオリジンはキーで拒否できる。起動時に WARN を1回出す（入口がキーだけである旨。キーの値は出さない） |
| Access のみ | リモート入口としては成立する | `verify_access_headers: true` かつ `api_key` が空。loopback は Access を経由しないため、同一 PC の別プロセスはキー無しで届く。起動時に WARN を1回出す。リモートの「URL を知っているだけ」を拒むのはエッジの Access に依存する |
| どちらも無し | 成立しない | `enabled: true` でも待受しない。ログは `paper-results-mcp: refusing to listen: no auth configured`（秘密は出さない） |

検査順は、待受が開いているリクエストに対して次のとおり。どちらも SQL より前に行う。

1. `verify_access_headers` が true のとき、2 つの Access ヘッダがローカル設定と一致しなければ `access_denied`。DB は開かない
2. `api_key` が非空のとき、`Authorization: Bearer` が一致しなければ `unauthorized`。DB は開かない
3. 両方を通過したときだけツールの SQL に進む

キーが無いことと違うことは、クライアントには同じ固定文で返す（どちらが空だったかは返さない）。提示された値はログ・標準出力・標準エラー・ツール戻り値に出さない。比較は定数時間で行う。

### 3.4 ローカル設定

ファイルは `%AppData%\pitha-trador\paper-results-mcp.json`。`secrets` テーブルにも `runtime_settings` にも置かない。根拠: #347 と同じくリモートから書き換えられる DB にスイッチを置かない。バックアップがコピーしうる DB と、入口の鍵を分ける。

| フィールド | 意味 | 既定 |
|------------|------|------|
| `enabled` | 待受を開くか | `false` |
| `listen` | 待受アドレス | `127.0.0.1:48110`。loopback 以外は起動失敗 |
| `api_key` | MCP 自身の API キー | 空。空は「キー因子なし」 |
| `verify_access_headers` | オリジンで Access ヘッダを照合するか | `true` |
| `access_client_id` | 照合する Client-Id | 空 |
| `access_client_secret` | 照合する Client-Secret | 空 |

ファイルの権限はユーザーのみ読み取り可（アプリの DB ファイルと同じ `0600`）。Git にコミットしない。値の例を仕様書・ログ・テストの失敗メッセージに埋め込まない。読み取れない・JSON でない場合はオフと同じく待受しない。

## 4. DB

読むファイルはアプリと同じ解決順（`PITHA_DB_PATH`、無ければ `DefaultDBPath`）。コピーを別に作って読まない。

- 開き方は `mode=ro`（`internal/repository/sqlitedb.PeekRuntimeSetting` が起動前の読み取りに使う DSN と同じ考え方）。`internal/repository/sqlitedb.Open` は使わない。根拠: `Open` はファイル作成・`PRAGMA journal_mode=WAL`・golang-migrate を行う
- 開いた接続で `PRAGMA query_only=ON` を実行する。失敗したら待受しない
- `PRAGMA wal_checkpoint` は一切実行しない。マイグレーションも実行しない。ファイルが無ければ作らず `db_unavailable`
- `busy_timeout` は 1 秒。アプリ本体の 60 秒（`sqlitedb` の `_busy_timeout=60000`）には合わせない。根拠: バックアップやチェックポイントでロックを待っている間、リモートの読み取りがアプリの書き込み側の待ちを長引かせない
- 接続は読み取り 1 本（`SetMaxOpenConns(1)`）。同時実行の上限は API 仕様の表
- 認証に失敗したリクエストは SQL を発行せず、接続も新規に開かない。起動時の疎通（`SELECT 1`）は、認証設定が §3.3 を満たして待受を開く場合に限り 1 回だけ行ってよい
- `secrets` への `SELECT` をツールからも疎通確認からも発行しない

書き込みを試みた SQL は SQLite が拒否する。その失敗をツールの成功応答にしてはならない（受け入れ条件のテストが、MCP の接続で `INSERT` がエラーになることを確認する）。

## 5. ツールの範囲

ツールは次の 8 つだけである。定義・フィールドは API 仕様。売買・発注・取消・設定変更・`runtime_settings` の更新・Kill Switch の操作はツールにしない。

| ツール | 読むもの |
|--------|----------|
| `paper_performance_summary` | 期間内のクローズ済み `positions` の件数・勝率・期待値・ドローダウン |
| `paper_daily_pnl` | クローズ日（JST）ごとの損益と件数 |
| `paper_trades` | クローズ済み取引のホワイトリスト列 |
| `jev_calibration` | `jev_decisions` と `calibration_outcomes` の件数・的中率・バケット・Brier |
| `metalabel_history` | `metalabel_models` の採用/却下と OOS AUC/EV、予測の take/skip 件数 |
| `risk_rejections` | `trade_signals.reject_reason` のうち Risk Engine 拒否の理由コード別件数 |
| `kill_switch_history` | `kill_switch_events` と `kill_switch_resolutions` のラベル |
| `collection_status` | `jobs` の件数、`market_snapshots` の鮮度と欠損日、`daily_bar_runs` の失敗件数、ラベルスキップ件数 |

新規テーブルは作らない。根拠: 必要な列はマイグレーション `000001`〜`000036` の既存表にある。

kabu の板・時価の保存を続けるかは #778 の現状のままである（保存は継続、リスクは理解済み、公式回答待ち）。本仕様は規約の適否を判断しない。リモートへ出す対象から生データを外すのは、その現状を広げないためである。

## 6. 非目標

- 実売買・Live の発注・口座の認証情報の読み書きを追加しない
- `market_snapshots.raw_data_json`、`jev_decisions.state_json` / `response_json`、板・気配・時価の列、`secrets`、API キーを戻り値にもログにも出さない（一覧は API 仕様の「出さないもの」）
- アプリの Gin ルート（セッション Cookie・CSRF・`/api/v1`）に本 MCP を相乗りさせない
- #347 のツール名（`kabu_station_status` / `kabu_api_error_guidance`）を本 MCP に生やさない

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-17 | 新規作成 | issue #783。読み取り専用 Paper 実績 MCP の仕様。実装は範囲外 |
