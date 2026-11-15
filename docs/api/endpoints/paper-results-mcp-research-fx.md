# API 仕様: Paper 実績 MCP の研究用の為替 1 分足の集計ツール

`docs/api/endpoints/paper-results-mcp.md` §3.35 の分割章（行数上限のため別ファイル）。issue #1084（USD/JPY の部分）。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。読み取り専用（設定も記録も書かない）。設計は `docs/architecture/paper-results-mcp-research-fx.md`、表は `docs/architecture/er/tables-research.md`。

日経平均・TOPIX など指数の系列は**まだ記録していない**（立花の日中の時価の記録 #1083 が使えるようになってから別に行う）。このツールが返すのは為替だけ。

## 3. ツール

### 3.35 `fx_series_summary`

研究用の為替 1 分足の記録（`research_fx_candles` / `research_fx_days`）が、通貨ペア（系列）ごとに何を持っているかを返す: 件数・最終時刻・欠け。**市場休み（土日・休日）と欠けを区別**して返す。**研究専用**で、取引の入力ではない。しきい値の判定・推奨は返さない。

引数: `from` / `to`（他のツールと同じ期間。省略は直近 30 日。最大 366 日。**GMO の取引日**（06:00 JST 切り替え）の `[from, to)` で絞る）、`symbol`（任意。`^[A-Z]{3}_[A-Z]{3}$`、例 `USD_JPY`。指定するとその通貨ペアだけを返す。省略は、記録のある全ペアと設定中のペア）。未知のキー・型違い・形式の違う `symbol` は `invalid_argument`、期間の不備は `invalid_period`。`additionalProperties` は false。

応答の `data`:

| キー | 内容 |
|---|---|
| `from` / `to` | 使った期間（JST の日付） |
| `notice` | 常に返す固定文: 研究専用で取引の入力ではない・GMO の公開 API の値で研究用にのみ使い再配布しない |
| `recording` | 記録の状態（下の表）。`runtime_settings` から読むだけ |
| `series[]` | 通貨ペアごと（記号の昇順）。下の表 |
| `definitions` | 各用語の定義（`trading_day` / `recorded_days` / `market_closed_days` / `missing_days` / `interior_gaps` など） |

`recording`:

| キー | 内容 |
|---|---|
| `enabled` | 記録のスイッチ（`research.fx.enabled`）。既定 `false`。壊れた値も `false` |
| `symbols` | 記録する通貨ペア（`research.fx.symbols`）。既定 `["USD_JPY"]` |
| `lookback_days` | 記録部品が見る取得期間（完了した直近の取引日の数）。7。これより古い日で取れていないものは欠けのまま |

`series[]` の要素:

| キー | 内容 |
|---|---|
| `symbol` / `configured` | 通貨ペアと、いま `research.fx.symbols` に入っているか |
| `candles` | 期間内の足（1 分）の数 |
| `bid_only_minutes` / `ask_only_minutes` | 売値（BID）だけ／買値（ASK）だけの足がある分の数 |
| `first_open_time` / `last_open_time` | 期間内の最初・最後の足の開始時刻（UTC の RFC3339。秒）。足が無ければ `null` |
| `days` | 取引日の内訳（下の表） |
| `interior_gaps` | 足のある日の中の欠け（下の表） |

`days`:

| キー | 内容 |
|---|---|
| `recorded` | 期間内で、足を 1 本以上記録した取引日の数 |
| `market_closed` / `market_closed_days` | 期間内で、GMO が答えたが足が 0 本だった取引日（土日・休日など市場休み）の数と日付の一覧。**欠けではない** |
| `missing` / `missing_days` | 記録を始めた取引日（`first_marked_day`）以降で、取引日が終わっているのに印の無い日の数と日付の一覧。**欠け**（まだ取っていない・取得に失敗した・取得期間を過ぎて取れなかった）。最初の印より前の日と、いまの取引日（まだ終わっていない）は数えない |
| `first_marked_day` | この通貨ペアの最初の印の取引日（期間によらない）。印が 1 つも無ければ `null`（このとき `missing` は 0） |
| `recorded_through` | 期間内の最新の印の取引日。無ければ `null` |

`interior_gaps`（足のある取引日の、最初の足と最後の足のあいだで、足が 1 本も無い分の連続）:

| キー | 内容 |
|---|---|
| `runs` / `missing_minutes` | 欠けの連続の数と、欠けた分の合計。**全件**の合計 |
| `list[]` | 連続した欠け。`{trading_day, start, end, minutes}`（`start`・`end` は欠けた最初・最後の分の開始時刻、UTC の RFC3339）。古い順で**最大 50 件** |
| `truncated` | `runs` が 50 を超え、`list` が全件でないとき `true` |

日の中の欠けは、市場休みの日（足が 0 本）や日をまたぐ区間を含まない。取引の少ない分に GMO が足を返さなかった場合も欠けとして出る（為替の 1 分足は通常は途切れない）。

エラー: `invalid_argument`（上記）、`invalid_period`、`query_timeout`（3 秒を超えたとき）、`db_locked` / `db_unavailable`（共通）。応答に価格・行単位の識別子は含まない。

例（要点だけ）:

```json
{"name": "fx_series_summary", "arguments": {"from": "2026-10-09", "to": "2026-10-18", "symbol": "USD_JPY"}}
```

```json
{"from": "2026-10-09", "to": "2026-10-18", "notice": "研究専用の記録の集計であり、取引の入力ではない。…",
 "recording": {"enabled": true, "symbols": ["USD_JPY"], "lookback_days": 7},
 "series": [{"symbol": "USD_JPY", "configured": true, "candles": 14, "bid_only_minutes": 1, "ask_only_minutes": 1,
   "first_open_time": "2026-10-08T21:00:00Z", "last_open_time": "2026-10-14T21:00:00Z",
   "days": {"recorded": 4, "market_closed": 2, "market_closed_days": ["2026-10-10", "2026-10-11"], "missing": 1, "missing_days": ["2026-10-13"],
            "first_marked_day": "2026-10-09", "recorded_through": "2026-10-15"},
   "interior_gaps": {"runs": 1, "missing_minutes": 3, "truncated": false,
     "list": [{"trading_day": "2026-10-12", "start": "2026-10-11T21:02:00Z", "end": "2026-10-11T21:04:00Z", "minutes": 3}]}}],
 "definitions": {"...": "..."}}
```

（数値は書式を示す架空の値。実データではない。）

## 3.33 `jev_evaluation` の `fx_symbol`

`jev_evaluation`（`paper-results-mcp-jev-evaluation.md` §3.33）の任意の引数 `fx_symbol` は、この記録を判断の時刻に合わせて結合する。仕様は `paper-results-mcp-jev-evaluation.md` に書く。引数を指定しなければ、`jev_evaluation` の応答はこの記録が無かった頃と同じ。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規（USD/JPY の部分。指数は未実装で #1083 待ち） | issue #1084 |
