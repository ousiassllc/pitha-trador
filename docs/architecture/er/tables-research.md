# ER / データモデル: テーブル定義（研究用のホライズンラベル）

`docs/architecture/er.md` から分割した章。対象: `research_horizon_labels` / `research_label_days` / `research_label_day_stats`（マイグレーション `000055`。issue #1074）。型・規約と全体 ER 図は `docs/architecture/er.md` を参照。設計は `docs/architecture/paper-results-mcp-research-labels.md`、読み取りツールは `docs/api/endpoints/paper-results-mcp-research-labels.md`。実装は `internal/repository/researchlabel`（表）・`internal/service/researchlabel`（記録部品）・`internal/bootstrap/researchlabeljob`（日次タスク）。

## 位置づけ

既存の 10 分・10bps のラベル（`calibration_outcomes`）に優位性が見えない（コスト前で +1〜2bps）ことを受け、保有時間を 30 分・60 分・引けまで変えたときの「コスト前・コスト後の bps」を、判断の有無によらず**全スナップショット**について残す、**研究専用**の記録。取引には使わない: Policy・Risk・発注・Paper ガード・Live・Jev のプロンプト・RAG・メタモデルの学習・自己改善は読まない（`internal/repository/researchlabel/isolation_test.go` が、参照できるファイルを記録部品・日次タスク・読み取り専用の MCP ツール `research_horizon_summary` に限って固定する）。

`market_snapshots`・`jev_decisions`・`calibration_outcomes`・`counterfactual_labels`・メタラベルの表には列も制約もトリガも足さない（既存行の数値は書き換えない）。この表だけを書き、読むのは `market_snapshots` の足（読み取り）だけ。

## research_horizon_labels

1 行は (スナップショット, ホライズン) の組。エントリーはそのスナップショットの足、出口は下の規則で決まる足。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| id | integer | PK（AUTOINCREMENT） | |
| snapshot_id | integer | NOT NULL（**外部キーなし**） | `market_snapshots.id`。`market_snapshots` は保持期間 90 日で削除されるので、FK だと削除が止まる・連鎖する。削除後もこの行は残る |
| instrument_id / symbol | integer / varchar(10) | NOT NULL | 銘柄 |
| snapshot_at | text | NOT NULL | スナップショットの時刻（UTC・固定 9 桁。`market_snapshots.timestamp` と同じ値）。判断（`jev_decisions`）との結合は `(instrument_id, snapshot_at = 判断の timestamp)` |
| day | text | NOT NULL, CHECK (長さ 10) | `snapshot_at` の JST の日付（`YYYY-MM-DD`）。日ごとの集計・標本読みのキー（記録部品が決める導出列） |
| sample_bucket | integer | NOT NULL, CHECK (0〜99) | `snapshot_id` の混ぜ関数（0〜99）。ツールが中央値を求めるとき、`sample_bucket < k` の行（k% の標本）だけを索引で読むための印 |
| horizon | text | NOT NULL, CHECK (`30m` / `60m` / `close`) | 出口の規則（下記） |
| exit_at | text | NOT NULL | 出口の足の時刻（UTC） |
| entry_bid / entry_ask / entry_price | numeric | bid・ask は NULL 可。price は NOT NULL, CHECK (`> 0`) | エントリーの足の板と価格。買いは ask、売りは bid で入る |
| exit_price | numeric | NOT NULL, CHECK (`> 0`) | 出口の足の `price` |
| spread_bps | numeric | NULL 可 | エントリーの足の `market_snapshots.spread_bps` |
| long_gross_bps / short_gross_bps | numeric | NULL 可 | コスト前の方向調整済みリターン。`(出口 - 入口) / 入口 × 10000 × 方向`（売りは符号反転。`calibration` の `grossReturnBps` と同じ。入口は買いが ask、売りが bid） |
| long_cost_bps / short_cost_bps | numeric | NULL 可 | 記録時の設定で既存のラベルがその側に置くコスト（`costsettings.LabelBarriers`: `calibration.barrier_cost_bps` を `cost.*` で動かした値。ライブのラベラーと同じ関数） |
| long_net_bps / short_net_bps | numeric | NULL 可 | `gross - cost` |
| label_version | integer | NOT NULL, CHECK (`= 1`) | 1 = この研究用の定義（固定ホライズンで決済。TP/SL バリアなし） |
| created_at | text | NOT NULL | 記録した時刻（UTC） |

制約・索引: `UNIQUE (snapshot_id, horizon)`。各側の 3 列は全部 NULL か全部非 NULL（CHECK）。その側に執行できる気配（買いは ask、売りは bid）が無い足は、その側の 3 列が NULL。両側とも無い足には行を作らない（CHECK: どちらかは非 NULL）。`(day, horizon, sample_bucket)`（日×ホライズンの読み）、`(instrument_id, snapshot_at)`（判断との結合）。

### 出口の規則

- `30m` / `60m`: エントリーの足の時刻 + 30/60 分以前の最後の足を出口とし、その足が目標時刻から `calibration.HorizonBarTolerance`（2 分）以内のときだけ行を作る。昼休み・引け・欠測で足が足りない窓は行を作らない（短い窓を 30/60 分の結果として記録しない。ライブのラベル `calibration.Labeler.Compute` と同じ規則）。
- `close`: 同じ JST 日の最後の足（15:30 以前）を出口とし、その時刻が **15:25 JST 以降**のときだけ行を作る（東証の大引けは 15:30。`marketcalendar` の `CloseAt`）。15:10 で足が止まった日には `close` の行を作らない。15:25 以降のエントリーには `close` の行を作らない（保有するものが残らない）。

## research_label_days

JST の日ごとの「記録を終えた」印と件数。記録部品は、その日の全銘柄の行を書き終え、日次の集計（次の表）を書いたあとに 1 行を足す。印の無い日は途中で止まったか未着手で、次の実行がやり直す（行は UNIQUE と `INSERT OR IGNORE` で二重にならない）。印の無い日の行は MCP ツールの集計に入れない（途中の日で偏らない）。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| day | text | PK, CHECK (長さ 10) | JST の日付 |
| snapshots | integer | NOT NULL, CHECK (`>= 0`) | その日に読んだ `market_snapshots` の足の数（削除後も残る分母） |
| labeled_30m / labeled_60m / labeled_close | integer | NOT NULL, CHECK (`>= 0`) | 印を付けた時点のホライズン別の行数 |
| completed_at | text | NOT NULL | 印を付けた時刻（UTC） |

## research_label_day_stats

日×ホライズン×側の和。印を付けるとき、その日の行から計算して印と同じトランザクションで書く（日を再記録すると置き換える）。MCP ツールは期間の平均・勝率・日単位のブートストラップをこの表から読む（日ごとの和がそのまま 1 まとまり）ので、数百万行を読まずに 3 秒の読み取りに収まる。`WITHOUT ROWID`。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| day / horizon / side | text | PK（3 列）。horizon は `30m`/`60m`/`close`、side は `LONG`/`SHORT` | |
| row_count | integer | NOT NULL, CHECK (`> 0`) | その側に結果のある行数（net が NULL でない行） |
| sum_gross / sum_net / sum_cost | numeric | NOT NULL | gross・net・コストの和（bps） |
| wins | integer | NOT NULL, CHECK (`0 〜 row_count`) | `net > 0` の行数 |
| spread_rows / sum_spread | integer / numeric | NOT NULL | `spread_bps` のある行数とその和 |
| spread_above_cost | integer | NOT NULL, CHECK (`0 〜 spread_rows`) | `spread_bps` > その行のコストの行数 |
| sum_gross_less_half_spread | numeric | NOT NULL | `spread_bps` のある行の `gross - spread_bps / 2` の和（`net_after_spread_bps` の分子） |

## 保持と量

`market_snapshots` は 90 日で削除されるが、この 3 表はそのままにしている（削除すると、足が無くなったあとに作り直せない結果が消えるため。方針は未決。`docs/architecture/paper-results-mcp-research-labels.md` §保持と量）。
