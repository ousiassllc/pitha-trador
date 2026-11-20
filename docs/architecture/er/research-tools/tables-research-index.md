# ER / データモデル: テーブル定義（研究用の指数の 1 分値）

`docs/architecture/er.md` から分割した章。対象: `research_index_minutes` / `research_index_days`（マイグレーション `000069`。issue #1084 の主要指数の部分）。設計は `docs/architecture/research-tools/paper-results-mcp-research-index.md`、読み取りツールは `docs/api/endpoints/research-tools/paper-results-mcp-research-index.md`（`index_series_summary`）と `jev_evaluation` の `index_symbol`。実装は `internal/repository/researchindex`（表）・`internal/service/researchindex`（記録部品）・`internal/bootstrap/researchindexjob`（日次タスク）。

## 位置づけ

判断の良し悪しを地合いで切り分けるための指数（日経平均・TOPIX など）の系列を、取引とは無関係に残す**研究専用**の記録。値は立花の日中の時価の記録（`research_quote_samples`、issue #1083。`er/tables-research.md`）に残っている指数コードの行から**写す**だけで、どこにも新しい要求を出さない（kabu の登録枠も使わない）。**既定オフ**（`runtime_settings` の `research.index.enabled`。オフの間は読み取りも書き込みもしない）。取引には使わない: Feature Engine・Policy・Risk・発注・Paper ガード・Live・Jev のプロンプト・RAG・メタモデルの学習・自己改善は読まない（`internal/repository/researchindex/isolation_test.go` が固定する）。

## research_index_minutes

1 行は (指数コード, 立会の 1 分)。`WITHOUT ROWID`。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| symbol | varchar(8) | PK（1 列目）, CHECK（長さ 1〜8 の英数字） | 立花の指数コード（`101`＝日経平均、`151`＝TOPIX など） |
| minute_time | text | PK（2 列目） | その分の開始時刻（UTC・固定 9 桁）。立会の分（09:00〜11:30、12:30〜15:30 JST）だけ |
| trading_day | text | NOT NULL, CHECK (長さ 10) | 東証の立会日（JST の日付）`YYYY-MM-DD`。導出列 |
| price | numeric | NOT NULL, CHECK (`> 0`) | 立花の時価の記録がその分の 5 秒目に取った値（`research_quote_samples.price`）。その分の始値でも終値でもない |
| via | text | NOT NULL, CHECK (`event` / `rest`) | 値の届き方（`research_quote_samples.via` と同じ） |
| recorded_at | text | NOT NULL | 写した時刻（UTC） |

制約・索引: `PRIMARY KEY (symbol, minute_time)`（同じ分を再度写しても行は増えない。`INSERT ... ON CONFLICT DO NOTHING`）。`(symbol, trading_day)`（日ごとの数え直しと期間の絞り込み）。

## research_index_days

立会日ごとの「確認した」印。記録部品は、その日の値を写した**同じトランザクション**で 1 行を足す（値だけがあって印が無い日は残らない）。「取れていない」と「まだ確認していない（欠け）」を分けるための表。`WITHOUT ROWID`。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| symbol / trading_day | varchar(8) / text | PK（2 列）。symbol は上と同じ CHECK、trading_day は CHECK (長さ 10) | 指数コードと立会日 |
| status | text | NOT NULL, CHECK (`recorded` / `unavailable`) | `recorded` = その日に値が 1 件以上あった。`unavailable` = 確認したが立花の記録に値が 1 件も無かった（**取れていない。市場休みではない**） |
| minute_count | integer | NOT NULL, CHECK (`>= 0`)、`(status = 'unavailable') = (minute_count = 0)` | 印を付けた時点のその日の行数（`unavailable` は 0） |
| checked_at | text | NOT NULL | 確認した時刻（UTC） |

### 欠けの定義

- 取得期間（直近 5 立会日。立会が終わった日だけ。15:35 JST 以降の今日を含む）の中で、**印の無い日**は「まだ確認していない・確認に失敗した」＝欠け。失敗した日は印を残さず、次の実行がやり直す。
- 土日・祝日・年末年始は立会が無いので、印も付けず欠けにも数えない（東証カレンダー `marketcalendar.TSE`）。
- `unavailable` の日は「取れていない」（立花の記録がオフ・指数コードが購読の対象でない・立花が値を返さなかった）。市場休みでも欠けでもない。印を付けた日は**最終**で、あとから立花の記録に値が入っても写し直さない。
- `recorded` の日の、最初の値と最後の値のあいだで、立会の分に値が 1 件も無い連続は「日の中の欠け」（`index_series_summary` の `interior_gaps`。昼休みは数えない）。

## 保持と量

この記録には削除（保持期間）を設けていない（立花の時価の記録は既定 90 日で退避・削除されるが、指数の値は研究のために長く残す）。1 コードで 1 日 330 行ぐらい、1 行は主キー込みで約 100 バイト [INFERENCE]。1 コード・1 年で約 8 万行・約 8MB。指数コードは最大 5 件。
