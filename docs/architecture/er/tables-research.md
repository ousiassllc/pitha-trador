# ER / データモデル: テーブル定義（研究用の記録: ホライズンラベル・立花の日足の収集・立花の時価）

`docs/architecture/er.md` から分割した章。対象: `research_horizon_labels` / `research_label_days` / `research_label_day_stats`（マイグレーション `000056`。issue #1074）、`research_tachibana_status` / `research_daily_bar_runs` / `research_daily_bar_progress`（`000059`。立花の日足の研究用収集。issue #1082。下の §立花の日足の研究用収集）、`research_quote_samples`（`000060`。立花の時価の記録。issue #1083。末尾の節）。以下の「位置づけ」から「保持と量」までは研究用ホライズンラベルの 3 表の説明。型・規約と全体 ER 図は `docs/architecture/er.md` を参照。設計は `docs/architecture/paper-results-mcp-research-labels.md`、読み取りツールは `docs/api/endpoints/paper-results-mcp-research-labels.md`。実装は `internal/repository/researchlabel`（表）・`internal/service/researchlabel`（記録部品）・`internal/bootstrap/researchlabeljob`（日次タスク）。

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

`market_snapshots` は 90 日で削除される。`research_horizon_labels` の行は、Settings の `research.labels.retention_days`（既定 90 日、1〜3650）より古い JST 日のものを日次タスクが削除する（索引 `(day, horizon, sample_bucket)` の範囲を 1000 行ずつ、間を置いて）。`research_label_days` と `research_label_day_stats` は削除しない（印は削除済みの日の再ラベルを防ぎ、日ごとの和は期間の平均を残す。1 日 7 行ほど）。記録そのものが Settings の `research.labels.enabled`（既定オフ）の下にあり、オフの間は書き込みも削除もしない。`docs/architecture/paper-results-mcp-research-labels.md` §5。

## 立花の日足の研究用収集（`research_tachibana_status` / `research_daily_bar_runs` / `research_daily_bar_progress`）

マイグレーション `000059`（issue #1082）。ブローカーが kabu のままでも、夜間に立花の日足を `daily_bars` へ記録し過去分を埋め戻す**研究専用・既定オフ**（`research.tachibana_bars.enabled`）の収集の状態と記録。取引には使わない: Policy・Risk・発注・Paper ガード・Live・Jev・Feature Engine・Scout・Trader は読まない（`internal/repository/researchbars/isolation_test.go` が参照できるファイルを収集・配線・読み取り専用の MCP ツール `research_daily_bars_status` に固定する）。`daily_bars` と `daily_bar_runs`（立花がブローカーのときの夜間バッチの記録）には列も制約も足さず、夜間バッチの記録とは混ぜない。設計は `docs/architecture/paper-results-mcp-research-bars.md`、読み取りツールは `docs/api/endpoints/paper-results-mcp-research-bars.md`（§3.36）。実装は `internal/repository/researchbars`・`internal/repository/researchstatus`・`internal/bootstrap/dailybars`（`Research`）。

### research_tachibana_status

研究用の収集ごとの現在の状態（1 成分 1 行。`bars` は本節、`quotes` は §research_quote_samples の記録）。状態が変わらないとき（毎分の `disabled` など）は書き換えない。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| component | text | PK, CHECK (`bars` / `quotes`) | |
| state | text | NOT NULL, CHECK (`disabled` / `not_configured` / `broker_is_tachibana` / `waiting` / `running` / `ok` / `error`) | スイッチがオフ / 認証情報が未設定 / 立花がブローカー / 実行を待っている / 実行中 / 成功 / 失敗 |
| detail | text | NOT NULL, 既定 `''` | 状態の平文の説明。**認証情報・URL・秘密鍵のパスは入れない**（未設定は「未設定です」とだけ書く） |
| updated_at | text | NOT NULL | 状態が変わった時刻（UTC） |

### research_daily_bar_runs

研究用の夜（`run_date` = `dailybars.NightOf`。8:00 までは前日の夜）ごとの 1 行。`daily_bar_runs` と同じ形で、100 銘柄ごとに保存する。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| run_date | text | PK, CHECK (`YYYY-MM-DD`) | 夜の日付 |
| status | text | NOT NULL, CHECK (`running` / `succeeded` / `failed`) | 成功 = 対象の全銘柄を試みた |
| started_at / finished_at | text | started_at NOT NULL。finished_at は実行中 NULL | UTC |
| symbols / requests / saved_bars / failed | integer | NOT NULL, 既定 0 | 対象銘柄数・リクエスト数・書いた日足の本数・失敗した銘柄数 |
| duration_ms | integer | NOT NULL, 既定 0 | 中断をまたいだ累計 |
| cursor | text | NOT NULL, 既定 `''` | 直近に試みた銘柄（再開は進み具合の表で決まり、これは記録用） |
| error | text | NOT NULL, 既定 `''` | 失敗の理由（短い文。認証情報を含まない） |

### research_daily_bar_progress

銘柄ごとの進み具合。再開と埋め戻しの印。`daily_bars` への外部キーは持たない（日足を削除しても残る）。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| symbol | varchar(10) | PK | |
| last_night | text | NOT NULL, CHECK (`YYYY-MM-DD`) | 最後に試みた夜。**その夜に試みた銘柄は同じ夜に取り直さない**（再起動・中断をまたぐ再開の仕組み）。次の夜は `last_night` の古い（未着手を先頭に）銘柄から歩く |
| last_ok | integer | NOT NULL, CHECK (0 / 1) | 最後の試みでブローカーが答えたか。0 = 失敗（`failed_symbols`） |
| first_ok_night | text | NULL 可, CHECK (`YYYY-MM-DD`) | 最初に成功した夜。NULL = 未埋め戻し。最初の成功が全履歴（最大約 20 年分）の取得 |
| bars_saved | integer | NOT NULL, 既定 0 | 研究用の収集が書いた日足の累計 |
| failures | integer | NOT NULL, 既定 0 | 失敗の累計 |
| updated_at | text | NOT NULL | UTC |

索引: `(last_night)`。量は銘柄数（約 4000）行で、1 夜に全銘柄を試みると 1 夜 4000 リクエスト（`broker.tachibana.nightly.max_per_second` の既定で 1 時間強）。

## research_quote_samples

立花 e支店の日中の時価を、kabu の `market_snapshots` と並べて比べるための**研究専用**の記録（マイグレーション `000060`。issue #1083）。ブローカーが kabu のまま、立花の EVENT 配信（足りない銘柄だけ REST の時価）から、立会中に 1 分に 1 回、計画した銘柄ごとに 1 行を書く。取引には使わない: Feature Engine・Scout・Trader・Paper・Policy・Risk・発注・Live・Jev のプロンプトは読まない（`internal/repository/researchquote/isolation_test.go` が、参照できるファイルを記録部品 `internal/bootstrap/researchquotes`・保持タスク `internal/bootstrap/researchquotejob`・退避 `internal/service/snapshotarchive`・読み取り専用の MCP ツール `research_quote_source_compare`・その登録と配線・マイグレーションだけに固定する）。`market_snapshots` には書かない（記録部品は `market_snapshots` の書き込み手段を持たない）。外部キーはない（保持で行を削除しても、退避ファイルが残る）。設計は `docs/architecture/paper-results-mcp-research-quotes.md`、読み取りツールは `docs/api/endpoints/paper-results-mcp-research-quotes.md`、要件は FR-META-11・FR-MCP-42。実装は `internal/repository/researchquote`。

| 列 | 型 | 制約 | 説明 |
|----|----|------|------|
| id | integer | PK, AUTOINCREMENT | 退避（`snapshotarchive`）が `id` と `timestamp` で日別ファイルを作る |
| timestamp | text | NOT NULL | 記録した**分**の瞬間（UTC。`market_snapshots.timestamp` と同じ書式の分切り捨て。`(source, symbol, timestamp)` が一意） |
| source | text | NOT NULL, CHECK (`tachibana` / `kabu`) | 現在の記録部品は `tachibana` だけを書く |
| symbol | varchar(10) | NOT NULL | 銘柄コード |
| price | numeric | NOT NULL, CHECK (`> 0`) | 現在値。価格のない銘柄の行は作らない（0 の行はない） |
| bid / ask / high / low / vwap / volume / turnover | numeric | NULL可 | 最良気配・当日高安・VWAP・累計出来高・累計売買代金（ブローカーが返したものだけ。VWAP の 0 は「なし」として NULL） |
| special_quote | integer | NOT NULL, DEFAULT 0, CHECK (`0` / `1`) | 特別気配 |
| observed_at | text | NULL可 | その値を最後に更新した時刻（EVENT の通知・REST の取得。静かな銘柄と切れた接続を分ける） |
| via | text | NOT NULL, CHECK (`event` / `rest`) | その分に使った最新の値の入り方 |
| session_phase | text | NOT NULL, CHECK (`open_auction` / `morning` / `lunch` / `afternoon` / `close`) | 立会の局面（9:05 前 / 〜11:25 / 〜12:35 / 〜15:20 / 15:20 以降。立会外は記録しない） |
| created_at | text | NOT NULL, DEFAULT | 記録時刻 |

索引: `(source, symbol, timestamp)` 一意（同じ分の再記録は `ON CONFLICT DO NOTHING` で増えない）、`(timestamp)`（保持と退避の範囲）。

### 保持と量

120 銘柄 × 約 330 分 ≒ 4 万行/日（1 分に 1 回の 1 トランザクションでまとめて書く）。Settings の `research.tachibana_quotes.retention_days`（既定 90 日、1〜3650）より古い日の行は、日次メンテナンスタスク `research_quote_retention`（`researchquotejob`）が、**削除の前に**ローカルへ退避して検証した日だけを 1000 行ずつ間を置いて削除する（`market_snapshots` と同じ #1072 の方式。保存先は DB と同じ階層の `snapshot_archive/research_quote_samples/YYYY/…csv.gz` と `manifest.json`。端末の外へは送らない。退避か検証に失敗したら何も削除しない）。記録のスイッチ（`research.tachibana_quotes.enabled`、既定オフ）に関わらず古い行は消えるが、表が空なら何も書かない。
