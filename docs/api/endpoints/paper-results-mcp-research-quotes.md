# API 仕様: Paper 実績 MCP の立花・kabu 時価の比較ツール

`docs/api/endpoints/paper-results-mcp.md` §3.36 の分割章（行数上限のため別ファイル）。issue #1083。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。読み取り専用（設定も記録も書かない）。設計は `docs/architecture/paper-results-mcp-research-quotes.md`、表は `docs/architecture/er/tables-research.md`（`research_quote_samples`）。

## 3. ツール

### 3.36 `research_quote_source_compare`

kabu の `market_snapshots` と、立花の研究用の時価の記録（`research_quote_samples`。立会中 1 分に 1 回・銘柄ごと）を並べて、**どれだけ同じ値か・どちらが欠けるか**を返す。取引の入力ではなく、しきい値の判定・採用の推奨・優劣の結論は含まない。記録は Settings の「立花の時価の記録（研究用）」（`research.tachibana_quotes.enabled`、既定オフ）がオンのときだけ動くので、オフの間は `days` が空で `status.state` が `disabled` になる。

引数: `from` / `to`（他のツールと同じ期間。省略は直近 30 日。最大 366 日）、`symbol`（任意。`^[0-9A-Za-z]{1,8}$` の銘柄コードで 1 銘柄に絞る）。未知のキー・型違い・書式違いの銘柄は `invalid_argument`。

対象は期間（JST の暦日）のうち、**立花の記録がある日**だけ。その日の kabu の `market_snapshots`（`symbol` を指定したときはその銘柄）と並べる。突き合わせの単位は 銘柄 × UTC の分（`timestamp` の先頭 16 文字）で、kabu はその分の**最後のスナップショットの `price`**、立花はその分の記録の `price`。

応答の `data`:

| キー | 内容 |
|---|---|
| `status` | 記録部品の状態 `{state, detail, updated_at}`（`research_tachibana_status` の `quotes` の行。`state` は `disabled` / `not_configured` / `broker_is_tachibana` / `waiting` / `running` / `ok` / `error`、行が無ければ `unknown`。`detail` は平易な説明で、認証情報・URL は入らない） |
| `symbol` | 指定した銘柄。無ければ `null` |
| `tachibana` / `kabu` | 合計のソース別の統計（下の表）。`days` の合計 |
| `comparison` | 合計の比較（下の表） |
| `by_phase[]` | 同じ比較を立会の局面（`open_auction` 9:05 前 / `morning` 〜11:25 / `lunch` 11:25〜12:35 / `afternoon` 〜15:20 / `close` 15:20 以降）別に、分のある局面だけ立会の順で返す（市場の文脈）。立花の行はその行の `session_phase`、kabu だけの分はその時刻から決める |
| `days[]` | 立花の記録がある JST 日ごとの `{date, tachibana, kabu, comparison}`。古い順 |
| `scan` | 読んだ範囲（下の表） |
| `definitions` | 各数値の定義 |
| `notice` | 常に返す固定文: 研究専用で取引の入力ではない |

ソース別の統計 `{rows, symbols, minutes, via_event, via_rest}`: 行数・銘柄数・UTC の分の数。`via_event` / `via_rest` は立花だけ（その分に使った最新の値が EVENT の通知か REST の取得か）。kabu は同じ分に複数行があれば行は別、分は 1 つ。

`comparison`:

| キー | 内容 |
|---|---|
| `minutes_both` | 立花と kabu の両方がある 銘柄 × 分 の数（対） |
| `only_kabu` / `only_tachibana` | kabu だけ（立花の記録がない）・立花だけ（kabu のスナップショットがない）の数 |
| `gap` | 対になった分の価格差 `(立花 − kabu) / kabu × 10000`（bps）: `pairs`、絶対値の `mean_abs_bps` / `median_abs_bps` / `p95_abs_bps`（最近順位） / `max_abs_bps`、符号つきの `mean_signed_bps`（正は立花が高い）、絶対値が 5・10・50bps を超えた対の割合 `share_above_5bps` / `_10bps` / `_50bps`。対が無いと `pairs` 0 で他は `null` |

`scan`:

| キー | 内容 |
|---|---|
| `rows_read` / `row_cap` | 両ソースで読んだ行数と、1 回の呼び出しで読む上限（40 万行） |
| `complete` | 上限か時間（`budget` のソフト期限）で止まったら `false`。止まったときは**その日の手前まで**（日の一部だけを数えない）を返し、`note` に理由 |
| `covered_through` | 読み終えた最後の JST 日（`YYYY-MM-DD`）。無ければ `null` |

呼び出し例:

```json
{"name": "research_quote_source_compare", "arguments": {"from": "2026-11-10", "to": "2026-11-15", "symbol": "7203"}}
```

読み取り専用（`mode=ro` の接続）。認証情報・URL・生の応答は返さない。
