# API 仕様: Paper 実績 MCP の研究用の指数の 1 分値の集計ツール

`docs/api/endpoints/paper-results-mcp.md` §3.51 の分割章（行数上限のため別ファイル）。issue #1084（主要指数の部分）。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。読み取り専用（設定も記録も書かない）。設計は `docs/architecture/research-tools/paper-results-mcp-research-index.md`、表は `docs/architecture/er/research-tools/tables-research-index.md`。

このツールが返すのは指数（既定は立花の指数コード `101`＝日経平均、`151`＝TOPIX）だけ。為替は `fx_series_summary`（`paper-results-mcp-research-fx.md` §3.46）。

## 3. ツール

### 3.51 `index_series_summary`

研究用の指数の 1 分値の記録（`research_index_minutes` / `research_index_days`）が、指数コード（系列）ごとに何を持っているかを返す: 件数・最終時刻・欠け。**取れていない日（`unavailable`）と、確認していない日（`missing`）と、立会の無い日を区別**して返す。**研究専用**で、取引の入力ではない。しきい値の判定・推奨は返さない。

引数: `from` / `to`（他のツールと同じ期間。省略は直近 30 日。最大 366 日。**東証の立会日**（JST の日付）の `[from, to)` で絞る）、`symbol`（任意。`^[0-9A-Za-z]{1,8}$`、例 `101`。指定するとその指数コードだけを返す。省略は、記録のある全コードと設定中のコード）。未知のキー・型違い・形式の違う `symbol` は `invalid_argument`。

応答の `data`:

| キー | 内容 |
|---|---|
| `from` / `to` | 使った期間（JST の日付） |
| `notice` | 常に返す固定文: 研究専用で取引の入力ではない・立花証券 e支店の時価から写した値で研究用にのみ使い再配布しない |
| `recording` | 記録の状態（下の表）。`runtime_settings` から読むだけ |
| `series[]` | 指数コードごと（昇順）。下の表 |
| `definitions` | 各用語の定義（`trading_day` / `minute` / `recorded_days` / `unavailable_days` / `missing_days` / `interior_gaps` / `source_samples` など） |

`recording`:

| キー | 内容 |
|---|---|
| `enabled` | 記録のスイッチ（`research.index.enabled`）。既定 `false`。壊れた値も `false` |
| `symbols` | 記録する指数コード（`research.index.symbols`）。既定 `["101", "151"]` |
| `lookback_days` | 記録部品が見る取得期間（完了した直近の立会日の数）。5。これより古い日で確認できていないものは欠けのまま |

`series[]` の要素:

| キー | 内容 |
|---|---|
| `symbol` / `name` / `configured` | 指数コードと、その名前（`101`＝日経平均、`151`＝TOPIX。ほかのコードは空文字）と、いま `research.index.symbols` に入っているか |
| `minutes` | 期間内の 1 分値の数 |
| `first_open_time` / `last_open_time` | 期間内の最初・最後の分の開始時刻（UTC の RFC3339。秒）。値が無ければ `null` |
| `source_samples` | 期間内に、立花の時価の記録（#1083）にこのコードの行がいくつあるか。0 なら、立花の記録に指数が入っていない（追加銘柄にコードを入れていない・記録がオフ） |
| `days` | 立会日の内訳（下の表） |
| `interior_gaps` | 値のある日の中の欠け（下の表） |

`days`:

| キー | 内容 |
|---|---|
| `recorded` | 期間内で、値を 1 件以上記録した立会日の数 |
| `unavailable` / `unavailable_days` | 期間内で、確認したが立花の記録に値が 1 件も無かった立会日の数と日付の一覧。**取れていない**（立花の記録がオフ・コードが購読の対象でない・立花が値を返さなかった） |
| `missing` / `missing_days` | 記録を始めた立会日（`first_marked_day`）以降で、立会日が終わっているのに印の無い日の数と日付の一覧。**欠け**（まだ確認していない・確認に失敗した・取得期間を過ぎて確認できなかった）。最初の印より前の日と、まだ終わっていない今日（15:35 JST まで）は数えない。土日・祝日・年末年始は立会が無いので、系列に現れず数えない |
| `first_marked_day` | このコードの最初の印の立会日（期間によらない）。印が 1 つも無ければ `null`（このとき `missing` は 0） |
| `recorded_through` | 期間内の最新の印の立会日。無ければ `null` |

`interior_gaps`（値のある立会日の、最初の値と最後の値のあいだで、立会の 1 分に値が 1 件も無い分の連続）:

| キー | 内容 |
|---|---|
| `runs` / `missing_minutes` | 欠けの連続の数と、欠けた分の合計。**全件**の合計 |
| `list[]` | 連続した欠け。`{trading_day, start, end, minutes}`（`start`・`end` は欠けた最初・最後の分の開始時刻、UTC の RFC3339）。古い順で**最大 50 件** |
| `truncated` | `runs` が 50 を超え、`list` が全件でないとき `true` |

立会は 09:00〜11:30 と 12:30〜15:30（JST、330 分）。昼休み（11:30〜12:30）は欠けに数えず、昼休みをまたぐ連続も分けて数える。値は、立花の時価の記録がその分の 5 秒目に取った値で、その分の始値でも終値でもない。

エラー: `invalid_argument`（上記）、`invalid_period`、`query_timeout`（3 秒を超えたとき）、`db_locked` / `db_unavailable`（共通）。応答に価格・行単位の識別子は含まない。

例（要点だけ）:

```json
{"name": "index_series_summary", "arguments": {"from": "2026-10-09", "to": "2026-10-17", "symbol": "101"}}
```

```json
{"from": "2026-10-09", "to": "2026-10-17", "notice": "研究専用の記録の集計であり、取引の入力ではない。…",
 "recording": {"enabled": true, "symbols": ["101", "151"], "lookback_days": 5},
 "series": [{"symbol": "101", "name": "日経平均", "configured": true, "minutes": 11, "source_samples": 2,
   "first_open_time": "2026-10-09T00:00:00Z", "last_open_time": "2026-10-16T06:29:00Z",
   "days": {"recorded": 3, "unavailable": 1, "unavailable_days": ["2026-10-13"], "missing": 1, "missing_days": ["2026-10-14"],
            "first_marked_day": "2026-10-09", "recorded_through": "2026-10-16"},
   "interior_gaps": {"runs": 2, "missing_minutes": 146, "truncated": false,
     "list": [{"trading_day": "2026-10-15", "start": "2026-10-15T00:02:00Z", "end": "2026-10-15T00:04:00Z", "minutes": 3}]}}],
 "definitions": {"...": "..."}}
```

（数値は書式を示す架空の値。実データではない。）

## 3.33 `jev_evaluation` の `index_symbol`

`jev_evaluation`（`paper-results-mcp-jev-evaluation.md` §3.33）の任意の引数 `index_symbol` は、この記録を判断の時刻に合わせて結合する。仕様は `paper-results-mcp-jev-evaluation.md` に書く。引数を指定しなければ、`jev_evaluation` の応答はこの記録が無かった頃と同じ。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-20 | 新規（主要指数の部分） | issue #1084 |
