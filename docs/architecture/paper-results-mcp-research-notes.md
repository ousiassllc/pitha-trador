# Paper 実績 MCP: 研究ノートの記録と読み取り（issue #1095）

`docs/architecture/paper-results-mcp.md` §5 の分割章（行数上限のため別ファイル）。ツールの引数と出力は `docs/api/endpoints/paper-results-mcp-research-notes.md` §3.36〜§3.39、表は `docs/architecture/er/tables-research.md`（マイグレーション `000057`）。

## 1. 目的と位置づけ

今は最適なやり方を探す期間で、AI（あいり）がいろいろ試す。調整の実験（`tuning_experiments`、`paper-results-mcp-experiments.md`）は設定を 1 つ変えたときの記録だけで、それ以外の調べもの（データの集計・バックテスト・比較台帳の結果・外部の情報）で分かったことはアプリの中に残らない。研究ノートは、その**問い・仮説・やったこと・結果の要約・結論（支持／不支持／保留）・根拠へのリンク（実験 ID・バックテストの結果 ID・比較台帳の条件・URL）・タグ**を蓄積し、後の改善や昇格の判断に人が読めるようにする。

## 2. 決定事項

- **記録だけ**: ノートの読み書きは設定・取引・Risk・発注・執行・ブローカー・Paper のガード・Live・Jev（trader-v3 と `JEV_*`）・既存のシャドー・Feature Engine に何も影響しない。ノートを書いても `runtime_settings` や取引の表は 1 行も変わらない（§5 のテスト）。
- **書く経路は MCP の `research_note_create` / `research_note_update` だけ**。実験のツールと同じく、`mcpset.Writer`（書き込み専用の狭い接続）が書き込み許可（`paper-results-mcp.md` §7.2。オフなら `write_disabled`）の下で、1 回 1 トランザクションで書く。収集・バックグラウンドのタスクは無い（AI が呼んだときだけ記録される）。Settings 画面は**読み取りだけ**（§6）。
- **追記が基本**: 作成は版 1、更新は現在の版 + 1 を `research_note_versions` に追記する。前の版は消さず `research_note_get` で読める。`research_notes` は現在の版の写し（一覧・検索が版の表を引かないための頭）で、更新のとき同じトランザクションで版の行を足したあとに `UPDATE ... WHERE id = ? AND current_version = ?` で差し替える。
- **版の行は書き換え・削除をしない**: コードに版を `UPDATE` / `DELETE` する文は無く（`researchnote` の隔離テストが固定）、表のトリガ（`research_note_versions_no_update` / `_no_delete`）も拒否する。ノート自体は削除する経路が無い（版の外部キーが `research_notes.id` を指す）。
- **楽観ロック**: `research_note_update` は `expected_version`（読んだときの `current_version`）が現在の版と違えば `research_note_version_conflict`（409）で何も書かない。同時に 2 人が更新しても、後から来た側が衝突を受けて読み直せる（書き手は 1 接続の `BEGIN IMMEDIATE` で直列）。
- **量の上限**: 問い 500・仮説 1000・やったこと 3000・結果 3000 文字、根拠リンク 20 件（`ref` 300 文字）、タグ 10 件（各 40 文字）、1 ノート 50 版。表の `CHECK` が文字数・件数・結論の値・JSON の形を守る。一覧は 1 回 100 件まで（既定 20）、`research_note_get` は 50 版まで（応答は数百 KB に収まる）。

## 3. 秘密の伏せ字

ノートの本文に秘密を書き込めないよう、**保存の前に**、自由文（問い・仮説・やったこと・結果の要約・タグ・根拠の `ref`）のキーの形の文字列を `[masked]` に置き換える（作成でも更新でも。`researchnote.Fields.Clean` → `researchnote/mask`）。版にも、全ツールの応答にも、一覧にも、伏せたあとの本文だけが出る。

| 形 | 例（架空） | 結果 |
|---|---|---|
| 既知の接頭辞 | `sk-…`・`ghp_…`・`github_pat_…`・`xox?-…`・`AKIA…`・`AIza…`・`sk_live_…` | その文字列を `[masked]` |
| Bearer など | `Authorization: Bearer abc…` | 方式名は残し、トークンを `[masked]` |
| JWT / 点区切り | `eyJ….eyJ….sig`、英数字 8 文字以上 ×3 の点区切りで数字を含むもの | `[masked]` |
| 代入の形 | `api_key=…`・`"password": "…"`・`token: …`・`secret=`・`passphrase`・`credential` | キーと区切りは残し、値を `[masked]` |
| URL の資格情報 | `https://user:pass@host` | パスワード部分を `[masked]` |
| PEM の秘密鍵 | `-----BEGIN … PRIVATE KEY-----` | 本体を `[masked]` |
| 長い不透明な文字列 | 32 文字以上の英数字・`+/=_-` で、英字と数字の両方を含む（16 進のハッシュ含む）、または 40 文字以上で大小文字が混ざる | `[masked]` |

- 方針は粗めで、誤検出（数字を含む長いスラッグが伏せられる）は許し、漏れを許さない。同じ入力は何度掛けても同じ結果（冪等。更新で変えない項目を再び掛けても変わらない）。普通の文章・URL・パス・日付・設定キー名は変えない。
- 文字数の上限は伏せる前（処理量の上限）と伏せたあと（`[masked]` のほうが長い場合）の両方で確かめる。検査の失敗の理由は値を引用しない。
- タグは伏せたあとに小文字にして重複を除く（接頭辞の判定が大文字小文字を区別するため）。
- `mcp_settings_audit` には書かない（実験のツールと同じ）。書いた人（`actor` = `mcp`）・接続元（`client`）・時刻は各版の行に残る。ログには失敗のコードしか出さない。

## 4. 検索

全文検索（FTS）は使わない。`research_notes_list` の `query` は、問い・仮説・やったこと・結果の要約・タグ（JSON 文字列）に対する `LIKE '%…%' ESCAPE '\'` で、`%` `_` `\` は先に `\` で打ち消して普通の文字として探す。英字の大文字小文字は区別しない（SQLite の既定）。タグは `json_each(tags)` で完全一致、期間は `updated_at` の範囲（JST の日付 `[from, to)` を UTC の固定 9 桁に直して文字列で比べる）。並びは `updated_at DESC, id DESC`。

## 5. テスト（受け入れ条件との対応）

| 条件 | テスト |
|---|---|
| MCP から書き込み・一覧・検索・更新ができ、更新前の版が残る | `notetests` の `TestCreateListSearchUpdateKeepsEarlierVersions`、`TestUpdateWithStaleExpectedVersionIsAConflict`、`TestNoteWritesFollowTheWriteSwitch`、`TestSearchTreatsLikeWildcardsAsPlainCharacters`、`TestInvalidRequestsStoreNothing`、`TestVersionsPerNoteAreBounded` |
| キーの形の文字列が伏せて保存される | `notetests` の `TestKeyShapedStringsAreMaskedBeforeStoring`（行・応答・版・監査表・検索）、`TestRefusalsDoNotEchoTheCallersText`、`researchnote/mask` の単体テスト、`researchnote` の `TestClean*` |
| 取引・Risk・発注・Jev・設定に差分がない | `notetests` の `TestNotesChangeNoSettingAndNoTradingState`（研究ノートの 2 表以外のすべての表を書き込み・読み取りの前後で比べる）、`TestNoteWritePathIsNarrow`（実行した書き込み文が 3 種類だけで、版の `UPDATE` / `DELETE` はトリガが拒否）、`researchnote/isolation_test.go`（表名・パッケージを参照できるファイルを限り、`researchnote` が import するのは SQL の補助と伏せ字だけ。Policy・Risk・発注・ブローカー・Jev・Feature Engine・設定のコードを呼べない） |
| マイグレーション | `db/migrationtests/migration_000057_test.go`（56↔57、既存の表に触れない、CHECK・UNIQUE・トリガ・外部キー） |
| Settings の一覧 | `internal/router/mcproute/research_notes_test.go`、`internal/web/handler/mcpsettings/research_notes_test.go` |

## 6. Settings 画面（読み取り専用）

MCP のカード（書き込みの節の下）に「研究ノート」の節を出す（`GET /settings/mcp/research-notes`。`internal/web/handler/mcpsettings/research_notes.go`、`internal/web/organisms/mcp/research_notes.templ`）。新しい順に 20 件、結論のバッジ（支持・不支持・保留）、問い、更新時刻（JST）、版の番号を並べ、開くと仮説・結果の要約（先頭 200 文字）・根拠リンクの件数・タグを見られる。フォームも書き込みの経路も無い。ゲートが読み取りを持たない構成（一部のテスト）ではこの節も経路も出ない。

## 7. 実装の置き場所

| 内容 | 場所 |
|---|---|
| 語彙・検査・伏せ字の呼び出し・SQL | `internal/service/paperresultsmcp/researchnote`（`note.go` / `store.go` / `read.go`）、伏せ字 `researchnote/mask` |
| 書き込み（`Writer`）と Settings 用の一覧（`Gate`） | `internal/service/paperresultsmcp/mcpset/research_notes.go` |
| ツール定義 | `internal/service/paperresultsmcp/researchnote/notetool`（4 ツールのスキーマ・説明・引数検査・エラー写像。サーバー本体は import しない）。ディスパッチ表への登録は `internal/service/paperresultsmcp/research_note_tools.go`（`notetool.Specs()` を `tool` に変換する） |
| エラーコード | `research_note_not_found`（404）・`research_note_version_conflict`（409）。`errors.go` と `paper-results-mcp-errors.md`。ツールの失敗は `query.CodedError`（`ErrorCode` / `ErrorDetail`）を実装した `notetool.Error` で返し、サーバーの `classify` が写す |
| 表 | `db/migrations/000057_create_research_notes.{up,down}.sql` |

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規 | issue #1095 |
