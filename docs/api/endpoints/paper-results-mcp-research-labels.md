# API 仕様: Paper 実績 MCP の研究用ホライズンラベルの集計ツール

`docs/api/endpoints/paper-results-mcp.md` §3.34 の分割章（行数上限のため別ファイル）。issue #1074。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。読み取り専用（設定も記録も書かない）。設計は `docs/architecture/paper-results-mcp-research-labels.md`、表は `docs/architecture/er/tables-research.md`。

## 3. ツール

### 3.34 `research_horizon_summary`

既存の 10 分・10bps のラベル（`calibration_outcomes`）に優位性が見えない（コスト前で +1〜2bps）ことを受け、保有時間を **30 分・60 分・引け**に変えたときの bps を、コスト前（gross）・コスト後（net = gross − 既存ラベルが置くコスト）で返す。対象は取引した判断だけでなく、scout の判断と**全スナップショット**（後から何でも評価できるように）。ラベルが仮定するコストを、実際のスプレッド（`market_snapshots.spread_bps`）と並べて見せる。**研究専用**で、取引の入力ではない。しきい値の判定・採用の推奨・優劣の結論は返さない。

引数: `from` / `to`（他のツールと同じ期間。省略は直近 30 日。最大 366 日）、`scope`（任意。`snapshots`（既定）/ `trader` / `scout`）、`direction`（任意。`LONG` / `SHORT`。`trader` / `scout` のときだけ指定でき、判断自身の方向で絞る。`snapshots` で指定すると `invalid_argument`）。未知のキー・型違い・上記以外の値は `invalid_argument`、期間の不備は `invalid_period`。

`scope`:

| 値 | 集計の対象 |
|---|---|
| `snapshots` | 期間内の全スナップショット。買い・売りの**両方**を返す（行ごとに両側が独立した結果で、方向を選んだ結果ではない） |
| `trader` / `scout` | その種別の判断（`LONG` / `SHORT`。`NONE` は除く）があった足。**判断自身の方向の側だけ**を返す。判断 1 件が 1 行（同じ足に判断が複数あればその数だけ）。判断と足は `(instrument_id, 判断の timestamp = スナップショットの時刻)` で結ぶ |

集計に入るのは、記録部品が**記録を終えた JST 日**（`research_label_days` の印のある日）の行だけ。途中で止まった日・未着手の日は入らず、`coverage` に出る。

応答の `data`:

| キー | 内容 |
|---|---|
| `scope` / `direction` | 使った値。`direction` は指定が無ければ `null` |
| `label_version` | 1（固定ホライズンで決済。TP/SL バリアなし） |
| `recorded_through` | 記録を終えた最新の JST 日（`YYYY-MM-DD`）。無ければ `null`。期間によらない |
| `horizons[]` | `30m` / `60m` / `close` の順に 1 要素ずつ。下の表 |
| `coverage` | 記録の網羅（下の表） |
| `definitions` | 各数値の定義（`entry` / `exit`（ホライズン別）/ `cost` / `net` / `spread` / `scope`（scope 別）/ `intervals`） |
| `notice` | 常に返す固定文: 研究専用で取引の入力ではない・しきい値の判定・採用の推奨・優劣の結論は含まない |

`horizons[]` の要素:

| キー | 内容 |
|---|---|
| `horizon` | `30m` / `60m` / `close` |
| `long` / `short` | その側の統計。その側の行が 1 件も無いと `null`（`trader` / `scout` では判断自身の方向の側だけが入り、`direction` を指定すると片側は常に `null`） |
| `cost_vs_spread` | `long` / `short` ごとの「ラベルが置くコスト」と「実際のスプレッド」（下の表）。`long` / `short` が `null` の側は `null` |

`long` / `short`（統計）:

| キー | 内容 |
|---|---|
| `rows` / `days` | 行数と JST 日数 |
| `mean_gross_bps` / `mean_net_bps` | コスト前・コスト後（net = gross − その行のコスト）の平均（bps）。全行 |
| `median_gross_bps` / `median_net_bps` | 同じ中央値（偶数件は中央 2 件の平均）。`median_sample_percent` が 100 未満のときは標本の値 |
| `median_sample_percent` | 中央値を求めた行の割合（%）。ホライズンの行が 10 万を超えると、`snapshot_id` のハッシュで選んだ k% の標本（平均・勝率・件数は常に全行）。100 = 全行で厳密。decision の scope は常に 100 |
| `win_rate` | `net > 0` の行の割合（0 は勝ちに数えない） |
| `mean_net_ci` | `mean_net_bps` の、JST 日を 1 まとまりに復元抽出した 2000 回のブートストラップ（固定シード）の 95% パーセンタイル区間 `{lower_bps, upper_bps}`。日数が 2 未満は `null` |
| `mean_net_se_bps` / `detectable_minimum_bps` | ブートストラップの標準誤差と、検出できる最小差 `(z(0.975) + z(0.8)) × SE`。固定の α=0.05（両側）・検出力 0.8 での**記述**で、採用のしきい値ではない。日数が 2 未満は `null` |

`cost_vs_spread.long` / `.short`:

| キー | 内容 |
|---|---|
| `rows` | その側の行数 |
| `assumed_cost.mean_bps` / `.median_bps` | ラベルが置くコスト（行の `long_cost_bps` / `short_cost_bps`＝記録時の設定で `costsettings.LabelBarriers` が出す値）の平均・中央値 |
| `rows_with_spread` | エントリーの足に `spread_bps` がある行数。下の 3 項目はこの行についてで、無ければ `null` |
| `entry_spread.mean_bps` / `.median_bps` | エントリーの足の `spread_bps` の平均・中央値 |
| `net_after_spread_bps` | `gross − spread_bps / 2` の平均。エントリーの気配（買いは ask、売りは bid）に入口側の半スプレッドは含まれるので、出口側の半スプレッドの代わりに `spread_bps / 2` を引く近似（出口の足の実スプレッドではない） |
| `share_spread_above_cost` | `spread_bps` がその行のコストを超えた行の割合（等しいときは超えない） |

`coverage`:

| キー | 内容 |
|---|---|
| `unit` | `snapshots`（`scope = snapshots`）/ `decisions`（`trader` / `scout`） |
| `in_period` | 期間内の数。`snapshots` は記録済みの日の足の数（記録時の値。`market_snapshots` が削除済みでも残る）に、未記録の日の足の数（`market_snapshots` の件数）を足したもの。`decisions` は `LONG` / `SHORT` の判断の数（`direction` で絞る） |
| `labeled` | ホライズン別の記録済みの行数（`decisions` は判断自身の側に結果のある判断の数）。行が無いホライズンはキーが無い |
| `snapshot_days[]` | 期間内で足のある JST 日ごとの `day` / `snapshots` / `labeled`（ホライズン別。**scope によらずスナップショット単位**）/ `recorded`（記録を終えた日か） |
| `unrecorded_days` | `snapshot_days` のうち、今日より前で `recorded` でない日の数。0 でなければ、その日は集計に入っていない |

引け（`close`）の出口は、同じ JST 日の最後の足（15:30 以前）が 15:25 JST 以降のときだけ。それより早く足が止まった日・15:25 以降のエントリーには `close` の行が無い。`30m` / `60m` は、目標時刻以前の最後の足が目標から 2 分以内のときだけ行があり、昼休み・引け・欠測をまたぐ窓は行が無い（短い窓を記録しない。ライブのラベルと同じ規則）。出口は出口の足の `price` で、出口側のスプレッドは含まない。

エラー: `invalid_argument`（上記）、`invalid_period`、`query_timeout`（3 秒を超えたとき）、`db_locked` / `db_unavailable`（共通）。応答に行単位の識別子・価格・応答本文は含まない。

例（要点だけ）:

```json
{"name": "research_horizon_summary", "arguments": {"from": "2026-11-01", "to": "2026-11-14", "scope": "trader", "direction": "LONG"}}
```

```json
{"scope": "trader", "direction": "LONG", "label_version": 1, "recorded_through": "2026-11-13",
 "horizons": [{"horizon": "30m",
   "long": {"rows": 84, "days": 9, "mean_gross_bps": 3.2, "mean_net_bps": -6.8, "median_gross_bps": 1.1, "median_net_bps": -8.9, "median_sample_percent": 100,
            "win_rate": 0.36, "mean_net_ci": {"lower_bps": -14.0, "upper_bps": 0.7}, "mean_net_se_bps": 3.9, "detectable_minimum_bps": 10.9},
   "short": null,
   "cost_vs_spread": {"long": {"rows": 84, "assumed_cost": {"mean_bps": 10, "median_bps": 10}, "rows_with_spread": 84,
     "entry_spread": {"mean_bps": 8.4, "median_bps": 7.1}, "net_after_spread_bps": -1.0, "share_spread_above_cost": 0.12}, "short": null}}],
 "coverage": {"unit": "decisions", "in_period": 91, "labeled": {"30m": 84, "60m": 80, "close": 77}, "snapshot_days": ["..."], "unrecorded_days": 0},
 "definitions": {"...": "..."}, "notice": "研究専用の記録の集計であり、取引の入力ではない。…"}
```

（数値は書式を示す架空の値。実データではない。）

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-14 | 新規 | issue #1074 |
