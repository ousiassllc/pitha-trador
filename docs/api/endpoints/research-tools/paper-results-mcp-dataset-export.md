# API 仕様: Paper 実績 MCP の学習用データセット書き出しツール

`docs/api/endpoints/paper-results-mcp.md` §3.45 の分割章（行数上限のため別ファイル）。issue #1094。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。設計は `docs/architecture/research-tools/paper-results-mcp-dataset-export.md`、データの仕様（列・ラベルの版・分割・manifest・ハッシュの確かめ方）は `docs/architecture/research-tools/training-dataset.md`。

## 3. ツール

### 3.45 `training_dataset_export`

期間内の `decision_type = 'trader'` の**全判断**（`NONE` を含む）を、1 判断 = 1 行の CSV と、同じ行・同じ列の Parquet の学習用データセットとして、**この PC のローカルフォルダ**に書き出す。応答は **manifest だけ**で、行の中身・秘密・設定の値は返さない。**研究専用**で、取引の入力ではない。MCP のツールのうちファイルを書く唯一のもの（DB は書き換えない）。注釈は `readOnlyHint: false`・`destructiveHint: false`。

**既定はオフ。** Settings の `research.export.enabled`（`runtime_settings` の JSON 整数 1 / 0、既定 0）がオンのときだけ書く。オフ（未設定・0・壊れた値）の間は何も書かない（フォルダも作らない）。Settings 画面か、MCP の `settings_set`（研究用のスイッチ 3 つだけがホワイトリストに入っている。保持期間・昇格の基準は入っていない）から変える。このツールがオンにすることはない。

書き先は引数で指定できない。DB ファイルと同じフォルダの `exports/<dataset_id>/`（`dataset_id` は内容のハッシュと期間・分け方から決まる）。フォルダは `0700`、ファイルは `0600`。

引数:

| キー | 型 | 内容 |
|---|---|---|
| `from` / `to` | 日付 | 他のツールと同じ期間（JST の日。`from` を含み `to` を含まない）。省略は直近 30 日。最大 366 日。`split_at` を渡すときは両方必須 |
| `split_at` | `YYYY-MM-DD`（任意） | 分割の境界（JST の日付）。`from` より後・`to` より前。判断の日が `split_at` より前は `train`、`split_at + embargo_days` 以降は `eval`。省略は分割なし |
| `embargo_days` | 整数 0〜30（任意、既定 0） | train と eval の間に空ける日数。`split_at` が必要。`split_at + embargo_days` は `to` より前 |

ラベルのホライズンは固定（保存済み calibration の 5・10・15・30・60 分、研究用の 30m・60m・close、見送り候補の反実仮想）で、引数では変えられない。未知のキー（`path` / `dir` など書き先を示すものを含む）・型違い・上記の範囲外・`split_at` の位置の誤りは `invalid_argument`、期間の不備は `invalid_period`。1 回に書き出せる判断は 20000 件まで（超えたら `limit_exceeded`。期間を狭める。何も書かない）。

分割（`split_at` あり）の規則（詳細は `training-dataset.md` §7）:

- `train` = 判断の日 < `split_at`。`eval` = 判断の日 >= `split_at` + `embargo_days`。間の日の判断は書き出さず `rows_in_embargo` に数える。
- train の行のうち、ラベルの窓（判断の時刻 + その行の最長のラベルのホライズン。`close` は同じ日の 15:30 JST）の終わりが eval の開始に届くものは書き出さず `rows_purged` に数える。
- `rows_train + rows_eval + rows_in_embargo + rows_purged = decisions`。分割なしは `rows = decisions`。

応答の `data`:

| キー | 内容 |
|---|---|
| `enabled` | スイッチの状態 |
| `written` | このとき新しくファイルを書いたか |
| `reason` | 書かなかった理由の固定コード。書いたときは空。`export_disabled`（オフ）/ `already_exported`（同じ内容のフォルダがすでにある。何も書き直さず、既存の manifest を返す）/ `dataset_dir_conflict`（同じ名前で内容の違うフォルダがある。上書きしない。今回の内容の manifest を返す） |
| `notice` | 常に返す固定文: ローカルだけに書いた・外部へ送っていない・データの中身は応答に含めない（オフのときはオフである旨） |
| `manifest` | オフのとき `null`。それ以外は下の表 |

`manifest`（`manifest.json` と同じ内容）:

| キー | 内容 |
|---|---|
| `schema_version` | `"2"` |
| `dataset_id` / `dataset_sha256` | `ds-` + 先頭 12 桁 / 全体のハッシュ（スキーマの版・期間 `from` / `to`・分割 `split_at` / `embargo_days`・各ファイルのハッシュから。期間か分割だけが違う書き出しは別の `dataset_id`） |
| `created_at` | 作成時刻（UTC RFC3339） |
| `from` / `to` / `split_at` / `embargo_days` | 実際に使った期間と分割（`split_at` は分割なしで `null`）。`dataset_sha256` に入る |
| `label_version` | `calibration`（2、`horizon_minutes`、`rows_at_other_horizons`）、`research_horizon`（1、`horizons`）、`counterfactual`（2）、`note` |
| `decisions` / `rows` | 期間の trader 判断の数 / 書き出した行数 |
| `rows_train` / `rows_eval` / `rows_in_embargo` / `rows_purged` | 上の規則の件数（分割なしは 0） |
| `train_days` / `eval_days` | 各側の判断の最初と最後の JST 日（`first` / `last`）。分割なしは `null` |
| `columns` | 列の名前（CSV のヘッダーと同じ順） |
| `excluded_columns` | 出さない列の一覧（生の板・気配・価格水準・応答の本文・自由文・設定・秘密） |
| `shadow_versions` / `shadow_versions_omitted` / `trial_arms` / `trial_arms_omitted` | シャドー（最大 3 版）・お試し（最大 4 アーム）のスロットに入った版・アーム（スロット順）と、上限で列にならなかった数 |
| `files[]` | `dataset_train.csv` / `dataset_train.parquet` / `dataset_eval.csv` / `dataset_eval.parquet`（分割なしは `dataset.csv` / `dataset.parquet`）ごとの `name` / `format`（`csv` / `parquet`）/ `rows` / `bytes` / `sha256`。同じ側の CSV と Parquet の `rows` は一致する |
| `save_dir` | 書き出したフォルダの絶対パス（パスは応答に出してよい） |
| `notice` | 同上 |

ファイルの SHA-256 が `files[].sha256` と一致し、`dataset_sha256` が `training-dataset.md` §10 の式で再現できる。同じ期間・分割・内容は同じバイト列・同じ `dataset_id` になる（Parquet もバイト単位で再現する）。応答は manifest だけで、Parquet の中身（行）も返さない。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規 | issue #1094 |
| 1.1 | 2026-11-19 | Parquet 出力（`files[].format`）、`dataset_id` に期間と分割を含める、`schema_version` 2 | issue #1121・#1124 |
