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
