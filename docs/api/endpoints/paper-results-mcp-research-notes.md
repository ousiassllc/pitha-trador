# API 仕様: Paper 実績 MCP の研究ノートツール

`docs/api/endpoints/paper-results-mcp.md` §3.36〜§3.39 の分割章（行数上限のため別ファイル）。issue #1095。設計・伏せ字・隔離は `docs/architecture/paper-results-mcp-research-notes.md`、表は `docs/architecture/er/tables-research.md`。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md`。

研究ノートは、調整の実験（`tuning_experiments`。設定 1 つの変更の記録）では残らない調べもの（データの集計・バックテスト・比較台帳・外部の情報）で分かったことを、問い・仮説・やったこと・結果・結論・根拠として蓄積する**記録専用**の表。**設定・取引・Risk・発注・Jev には何も影響しない**。書き込みは追記で、更新すると新しい版を足し、前の版は消さない。

## 3. ツール

### 3.36 `research_note_create`

ノートを 1 件記録する（版 1）。書き込み許可（`paper-results-mcp.md` §7.2）がオフなら `write_disabled`（403）で何も保存しない（実験のツールと同じ）。

| 引数 | 必須 | 内容 |
|------|------|------|
| `question` | ○ | 問い（1〜500 文字。改行・タブ可、そのほかの制御文字は不可） |
| `hypothesis` | | 仮説（〜1000 文字） |
| `actions` | | やったこと（〜3000 文字） |
| `result_summary` | | 結果の要約（〜3000 文字） |
| `conclusion` | | `supported`（支持）/ `unsupported`（不支持）/ `pending`（保留。既定） |
| `evidence_links` | | 根拠へのリンクの配列（最大 20 件）。要素は `{kind, ref}`、`kind` は `experiment_id` / `backtest_result_id` / `comparison_ledger` / `url` / `other`、`ref` は 1〜300 文字（実験 ID・バックテストの結果 ID・比較台帳の条件・URL など） |
| `tags` | | タグの配列（最大 10 件、各 1〜40 文字）。小文字にして重複を除いて保存する |

未知の引数・要素の未知のキーは `invalid_argument`（422。理由は `error.data.detail` に載るが、送った値は引用しない）。**キーの形の文字列**（API キー・トークン・パスワード・Bearer・JWT・`api_key=…` など）は保存の前に `[masked]` に置き換える（文字数の上限は伏せたあとにも守る）。

応答の `data`: 作ったノート `{id, question, hypothesis, actions, result_summary, conclusion, evidence_links[], tags[], current_version(1), created_at, updated_at}`（伏せたあとの本文）。実行文は `research_notes` と `research_note_versions` への `INSERT` 各 1 件だけ（同じトランザクション）。`mcp_settings_audit` には書かない（書いた人と接続元は版の行の `actor` / `client`）。

### 3.37 `research_note_update`

ノートを更新する。渡した項目だけを差し替えた**新しい版**（現在の版 + 1）を追記する。前の版は消さない。

| 引数 | 必須 | 内容 |
|------|------|------|
| `note_id` | ○ | ノート ID（1 以上） |
| `expected_version` | ○ | いま見ている版（`current_version`）。違うと `research_note_version_conflict`（409）で何も書かない。`error.data.detail` に現在の版 |
| `question` / `hypothesis` / `actions` / `result_summary` / `conclusion` / `evidence_links` / `tags` | | `research_note_create` と同じ規則。渡さない項目は現在の値のまま。`evidence_links` / `tags` に `[]` を渡すと空にする |

- 項目が 1 つも無い・現在の版と同じ内容になるときは `invalid_argument`（版を増やさない）。ない ID は `research_note_not_found`（404）。1 つのノートの版は 50 まで（超えると `invalid_argument`。新しいノートを作る）。書き込み許可がオフなら `write_disabled`。
- 伏せ字は更新でも同じに掛かり、版にはその伏せたあとの本文が残る。
- 応答の `data`: 更新後のノート（`research_note_create` と同じ形）。実行文は `research_note_versions` への `INSERT` 1 件と、`research_notes`（現在の版の写し）の `UPDATE` 1 件（`WHERE id = ? AND current_version = ?`）。版の行の `UPDATE` / `DELETE` はコードにも無く、表のトリガも拒否する。

### 3.38 `research_notes_list`

ノートの一覧（更新の新しい順。**読み取り専用**）。軽い行（現在の版）を返す。

| 引数 | 内容 |
|------|------|
| `query` | 探す文字列（〜200 文字）。問い・仮説・やったこと・結果の要約・タグを、英字の大文字小文字を区別せず部分一致で探す（`LIKE`。`%` `_` `\` はそのままの文字として探す。日本語は完全一致の部分文字列） |
| `tag` | タグ 1 つ（小文字にして完全一致） |
| `conclusion` | `supported` / `unsupported` / `pending` |
| `from` / `to` | 更新時刻の JST の日付 `[from, to)`（`YYYY-MM-DD`）。他の期間つきツールと同じ規則（両方指定するか両方省略。期間は最大 366 日。片方だけ・不正は `invalid_period`）で、**省略は期間で絞らない** |
| `limit` | 1〜100、既定 20。超過は `limit_exceeded` |

応答の `data`: `{notes: [{id, question, hypothesis(先頭 200 文字), result_summary(先頭 200 文字), conclusion, tags[], evidence_count, current_version, created_at, updated_at}]}`。長い本文は先頭 200 文字で切り、`…` を付ける（全文は `research_note_get`）。

### 3.39 `research_note_get`

ノート 1 件の全文と、すべての版（**読み取り専用**）。引数: `note_id`（○）。ない ID は `research_note_not_found`（404）。

応答の `data`: `{note: {現在の版。§3.36 と同じ形}, versions: [{version, question, hypothesis, actions, result_summary, conclusion, evidence_links[], tags[], actor("mcp"), client, created_at}]}`。`versions` は古い順で、各版は全文（その版の内容。最大 50 版）。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規（`research_note_create` / `research_note_update` / `research_notes_list` / `research_note_get`） | issue #1095 |
