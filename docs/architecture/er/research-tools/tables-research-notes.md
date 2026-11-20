# ER / データモデル: テーブル定義（研究ノート）

`docs/architecture/er.md` から分割した章。対象: `research_notes` / `research_note_versions`（マイグレーション `000067`。issue #1095）。型・規約と全体 ER 図は `docs/architecture/er.md` を参照。設計は `docs/architecture/research-tools/paper-results-mcp-research-notes.md`、ツールは `docs/api/endpoints/research-tools/paper-results-mcp-research-notes.md`。実装は `internal/service/paperresultsmcp/researchnote`（SQL・検査）と `.../researchnote/notetool/write.go`（書き込み）。

## 位置づけ

AI が調べたこと（問い・仮説・やったこと・結果の要約・結論・根拠へのリンク・タグ）を蓄積する**記録専用**の 2 表。調整の実験（`tuning_experiments`）が残さない調べもの（データの集計・バックテスト・比較台帳・外部の情報）の結果を残す。取引には使わない: Policy・Risk・発注・執行・ブローカー・Paper ガード・Live・Jev のプロンプト・RAG・メタモデルの学習・自己改善・Feature Engine・設定は読まない（`internal/service/paperresultsmcp/researchnote/isolation_test.go` が固定）。既存の表には列も制約もトリガも足さない。時刻は UTC の RFC3339・固定 9 桁（`er.md` §型・規約「日時」）。

追記が基本: 作成は版 1、更新は新しい版を `research_note_versions` に追記し、前の版は消さない。`research_notes` はその時点の現在の版の写し（頭）。

## research_notes

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| id | integer | PK（AUTOINCREMENT） | |
| question | text | NOT NULL, CHECK (空白だけでなく、500 文字以内) | 問い |
| hypothesis | text | NOT NULL DEFAULT `''`, CHECK (1000 文字以内) | 仮説 |
| actions | text | NOT NULL DEFAULT `''`, CHECK (3000 文字以内) | やったこと |
| result_summary | text | NOT NULL DEFAULT `''`, CHECK (3000 文字以内) | 結果の要約 |
| conclusion | text | NOT NULL DEFAULT `pending`, CHECK (`supported` / `unsupported` / `pending`) | 結論。支持・不支持・保留 |
| evidence_links | text | NOT NULL DEFAULT `[]`, CHECK (JSON 配列・20 件以内・20000 文字以内) | 根拠へのリンクの JSON 配列。要素は `{"kind": …, "ref": …}`（`kind` は `experiment_id` / `backtest_result_id` / `comparison_ledger` / `url` / `other`。`ref` は 1〜300 文字） |
| tags | text | NOT NULL DEFAULT `[]`, CHECK (JSON 配列・10 件以内・1200 文字以内) | タグの JSON 配列（各 1〜40 文字。小文字・重複なし） |
| current_version | integer | NOT NULL DEFAULT 1, CHECK (`>= 1`) | 現在の版。更新は `WHERE id = ? AND current_version = ?` で差し替える（楽観ロック） |
| created_at / updated_at | text | NOT NULL | 作成時刻と最後の更新の時刻（UTC・固定 9 桁） |

索引: `research_notes_updated_idx (updated_at, id)`（一覧の並びと期間の絞り込み）。自由文（問い・仮説・やったこと・結果・タグ・根拠の `ref`）は、保存の前にキーの形の文字列を `[masked]` に置き換える（アプリ側。表の `CHECK` は文字数と件数の上限だけを守る）。

## research_note_versions

追記専用。1 版 1 行で、その版の**内容の全体**を持つ（前の版との差分ではない）。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| id | integer | PK（AUTOINCREMENT） | |
| note_id | integer | NOT NULL, FK → `research_notes.id` | ノート。ノートを削除する経路は無い（アプリの接続では外部キーが版のあるノートの削除を拒む） |
| version | integer | NOT NULL, CHECK (`>= 1`), UNIQUE (`note_id`, `version`) | 版の番号。1 から連続する |
| question … tags | | `research_notes` と同じ列・同じ制約 | その版の内容 |
| actor | text | NOT NULL, CHECK (`mcp`) | 書いた人。MCP（Bearer 認証を通ったクライアント）だけ |
| client | text | NOT NULL DEFAULT `''` | 接続元ホストの IP。ヘッダの値・API キーは入れない |
| created_at | text | NOT NULL | その版を書いた時刻 |

追記専用は**トリガで**守る（コードにも書き換える文は無い）。`research_note_versions_no_update` / `research_note_versions_no_delete` が版の行の `UPDATE` / `DELETE` を、`research_note_versions_no_replace`（`BEFORE INSERT`）が同じ `(note_id, version)` または同じ `id` の行がすでにある `INSERT`（`INSERT OR REPLACE` / `REPLACE INTO` を含む）を `ABORT` で拒否する。外部キーは `research_notes.id` を指し、外部キーを有効にした接続（アプリの接続）では版のあるノートは `DELETE` できない。外部キーの強制は接続の設定によるので、書き込み専用の接続が有効にしているかには頼らない。

## 保持と量

保持のパージは無い（Retention の対象外。ノートは AI が呼んだときだけ増え、1 ノートは 50 版まで）。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | マイグレーション 000067 で新規（`research_notes` / `research_note_versions`） | issue #1095 |
| 1.1 | 2026-11-15 | 版の表に`research_note_versions_no_replace`（`BEFORE INSERT`。同じ `(note_id, version)` / `id` の `INSERT`・`REPLACE` を拒否）を足し、追記専用はトリガで守ること、ノートを削除できないのは外部キーを有効にしたアプリの接続での話であることを明記 | issue #1095 レビュー修正 |
