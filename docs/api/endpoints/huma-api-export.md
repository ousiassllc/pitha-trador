# API仕様: API ルート（§5 続き: データエクスポート）

`docs/api/endpoints/huma-api.md`（§5）から分割した節（300行/ファイル制限のため）。節番号は変更なし。Humaの共通方針・エラー形式は`huma-api.md`を参照。

### GET /api/v1/export

取引ログ・判断ログ・計測ログのエクスポート（`requirements/functional/components-platform.md` §4.21 / FR-EXPORT-1〜7）。期間と種類を指定して`internal/service/dataexport`でCSVまたはJSONLのZIP（種類ごとの`<種類>/<テーブル>.<csv|jsonl>`と`manifest.json`）を作り、添付ファイルとして返す読み取り専用API。Settings画面`#data-export-panel`のフォームが`GET`で直接呼ぶ（JSを介さないブラウザ標準のダウンロード）。新規テーブルは持たない。

| クエリ | 型 | 説明 |
|-------|-----|------|
| `from` | date（`YYYY-MM-DD`） | 期間の開始日（UTC、含む）。必須 |
| `to` | date（`YYYY-MM-DD`） | 期間の終了日（UTC、含む）。必須。内部では`[from, to+1日)`として扱う |
| `kinds` | string（繰り返し） | `trades`（取引ログ）/`decisions`（判断ログ）/`metrics`（計測ログ。INFO以上）。`kinds=trades&kinds=metrics`の形で1つ以上必須 |
| `format` | string | `csv`（既定）/`jsonl` |

応答（200）はZIPの作成が終わってから送信する（失敗時に壊れたZIPを返さないため。一時ファイルに作成し、期間全体はメモリに保持しない）:

| ヘッダ | 内容 |
|-------|------|
| `Content-Type` | `application/zip` |
| `Content-Disposition` | `attachment; filename="pitha-export-20261015-123045.zip"`（UTCの取得時刻） |
| `Content-Length` | ZIPのバイト数 |

0件の期間でもヘッダ行付きの空ファイルと行数0の`manifest.json`を返す。`manifest.json`の`files[].truncated`は、計測ログが10MiBを超えて古い側を切り捨てたときのみ`true`。秘密情報は`[REDACTED]`に置換済み（FR-EXPORT-5）。

422: `from`/`to`/`kinds`の欠落、日付形式の不正、`kinds`/`format`が許容値外、`from`が`to`より後、期間が366日を超える（`to - from`が366日以上）場合。500（固定メッセージ。原因はslogへ。`api/endpoints.md` §7）は、エクスポートの失敗時、およびエクスポータ未注入のとき（`router.WithDataExporter`の組み立て漏れ。未指定のときは常に失敗するエクスポータ（`export.UnconfiguredExporter`）が使われる）。Setup Guardの対象で、必須認証情報が未設定の間は503 JSON。

認証: `GET /api/v1/logs/errors`と同じく、このGETは有効なセッションCookieを必須とし、無ければ403（`missing or invalid session cookie`。`api/endpoints.md` §1）。ダウンロードは操作者の操作としてハートビート更新対象（FR-RISK-6）。

### GET /api/v1/migration/export ・ POST /api/v1/migration/preview ・ POST /api/v1/migration/confirm ・ POST /api/v1/migration/cancel ・ GET /api/v1/migration/status

PC 移行（`requirements/functional/components-platform.md` §4.25 / FR-MIGRATE-1〜7）。Settings「PC 移行」カード（`#migration-panel`）が使う。分析用の`/export`とは別の、**復元できる**移行ファイル（`.pitha-bundle`＝`manifest.json`＋秘密を除いたSQLiteの写し`pitha.db`のZIP）を扱う。アプリは稼働中のDBを入れ替えない。取り込みは「確認→予約→次回起動で適用」の 2 段階。いずれも有効なセッションCookieが必須（無ければ403 `missing or invalid session cookie`）で、`POST`はCSRFトークンも必須（`api/endpoints.md` §1）。Setup Guardの対象で、必須認証情報が未設定の間は503 JSON。

| メソッド・パス | 内容 |
|--------------|------|
| `GET /migration/export` | 移行ファイルを作って添付で返す。一貫スナップショットを一時ファイルへ作り、scrub・`integrity_check`・SHA-256・行数の記録まで済んでから送信する（失敗時に壊れたファイルを返さない）。`Content-Type: application/octet-stream`、`Content-Disposition: attachment; filename="pitha-migration-20261114-123045.pitha-bundle"`（UTCの取得時刻）、`Content-Length`。500は書き出し失敗・未注入（`router.WithMigration`の組み立て漏れ。未指定は常に失敗する`migration.Unconfigured`） |
| `POST /migration/preview` | 本文は**ファイルそのもの**（`application/octet-stream`。multipartではない。huma外の素の gin ルートで、本文はメモリに溜めず一時ファイルへ流す。上限4GiB→413）。形式版・スキーマ版の互換性を最初に確認し、次にSHA-256・`integrity_check`・manifestとの一致・秘密情報が入っていないことを検証して、検証済みDBを`migration/staging/`へ置き、`{token, manifest, tables[{name,current,bundle}], current_schema_version, target_has_data}`（200 JSON）を返す。現在のデータは変わらない。直前の未予約の確認は破棄される（同時に1つ）。422（`application/problem+json`、`detail`は日本語）: ZIPでない・構成が違う（`ErrNotBundle`）、形式版が違う、ファイルのスキーマが実行中のアプリより新しい、SHA-256/サイズ/`integrity_check`/行数の不一致（壊れた・書き換えられた）、秘密情報または端末固有の設定が入っている |
| `POST /migration/confirm` | 本文`{"token": "<preview の token>", "replace_existing": false}`。現在のDBにユーザーデータがあるとき`replace_existing: true`が無ければ409。トークン不明・期限切れ（新しい確認で破棄済み）も409。成功（200）で`{manifest, confirmed_at, replaced_existing}`を返し、検証済みDBを`migration/pending/`へ移す。次の起動で適用（FR-MIGRATE-5） |
| `POST /migration/cancel` | 確認中のファイルと予約済みの取り込みを破棄する（200） |
| `GET /migration/status` | `{pending?, last_result?}`。`pending`は予約済み（再起動待ち）の取り込み、`last_result`は直近の起動時の適用結果（`status: applied|failed`・`at`・`error`・`safety_copy`・ファイルの作成日時とバージョン） |

500（固定メッセージ。原因はslogへ。`api/endpoints.md` §7）は上記以外の失敗。ステージング領域・保留領域・安全コピー・結果ファイルはDBファイルの隣の`migration/`（`0700`/`0600`）にある。