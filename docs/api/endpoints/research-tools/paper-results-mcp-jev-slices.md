# API 仕様: Paper 実績 MCP の Jev 評価の切り分けツール

`docs/api/endpoints/paper-results-mcp.md` の分割章（行数上限のため別ファイル）。issue #1089（前半: 切り分け）。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` と `docs/architecture/paper-results-mcp-errors.md` に従う。**読み取り専用**（`readOnlyHint: true`・`destructiveHint: false`。何も書かず、設定も変えない）。設計は `docs/architecture/research-tools/paper-results-mcp-jev-slices.md`。

## 3. ツール

### 3.35 `jev_evaluation_slices`

`jev_evaluation`（`paper-results-mcp-jev-evaluation.md` §3.33）の **llm アーム**（判断自身の方向のコスト控除後 net bps）を、特徴で切り分けて返す。母集団・ラベル・cutoff・`(model_id, question_version)` ごとの別集計は `jev_evaluation` と同じ行（`evaldiag.Rows`）で、コホートごとの見出し（`all`）は `jev_evaluation` の llm の `all` と同じ数字になる。

**位置づけ**: 研究用の読み取りの集計。採用・昇格の基準ではなく、しきい値・合否・推奨は返さない。バケットは**固定の順**で並べ、結果で並べ替えず、「最良のバケット」を示さない。Jev・Risk・Policy・注文・Paper ガード・メタモデルの学習・既存のシャドー・既存の表の中身は変えず、結果をそれらに渡さない。読む表は `jev_evaluation` と同じ（`jev_decisions`・`instruments.sector`・`market_snapshots`・`trade_signals`・`calibration_outcomes`・`runtime_settings` の固定キー）。

引数:

| 引数 | 内容 |
|---|---|
| `from` / `to` | 他のツールと同じ期間（JST の暦日、`[from, to)`、最大 366 日、既定は直近 30 日）。判断の時刻で切る |
| `horizon_minutes` | 任意。ラベルの保有時間（分、1〜60）。省略はいまの `calibration.barrier_timeout_minutes`。`jev_evaluation` と同じ検証 |
| `prompt_version` | 任意。この版（`question_version`）の判断だけ。`jev_evaluation` と同じ検証 |
| `model_id` | 任意。この `model_id` の判断だけ（100 文字まで）。`jev_evaluation` と同じ検証 |
| `dimensions` | 任意。返す切り口の名前の配列（下の 9 つのどれか。重複・空配列・未知の名前は `invalid_argument`）。省略は全部。**絞っても Holm 補正の family は全切り口のまま**（`family_size`・`holm_adjusted_p` は変わらない） |

不明な引数・型の違い・範囲外・未知の切り口名は `invalid_argument`（422）、期間の誤りは `invalid_period`（422）。期間内の判断が `evaldiag.MaxDecisions`（10,000。窓の行数の上限 `MaxWindowRows` から `horizon_minutes` が長いほど少なくなる）を超える、または `(model_id, question_version)` の組が 50（`MaxCohorts`）を超えると、部分結果を返さず `limit_exceeded`（422。期間か `model_id` / `prompt_version` で狭める）。

## 切り口とバケット

どの切り口も、コホートの評価行（`jev_evaluation` の `evaluated`）を**ちょうど 1 つのバケットに**入れる。したがって切り口ごとのバケットの `n` の合計はコホートの `evaluated` に一致する（`rows` にも同じ数）。

**事前登録（`preregistered`）— 2026-11-15、issue #1089 でコードに固定した切り口。バケットの境界もこの日に固定し、結果を見て動かさない。**

| 切り口 | バケット（出力の順） |
|---|---|
| `regime` | `jev_decisions.market_regime`: `trend` / `mean_revert` / `unknown`（記録なし・それ以外の値） |
| `time_of_day` | 判断の JST の時刻。各区間は開始を含み終了を含まない: `open` 09:00–09:30（寄り直後）/ `morning` 09:30–11:30（前場）/ `afternoon` 12:30–14:30（後場）/ `pre_close` 14:30–15:30（引け前）/ `other`（昼休み・時間外など） |
| `direction` | 判断自身の方向: `LONG` / `SHORT` |
| `confidence_band` | `jev_decisions.confidence` の固定の帯: `[0.0,0.6)` / `[0.6,0.7)` / `[0.7,0.8)` / `[0.8,0.9)` / `[0.9,1.0]`（下端を含み上端を含まない。最後だけ 1.0 を含む）/ `unknown`（記録なし・0〜1 の外） |

**後から見た切り口（`exploratory`）— 結果を見た後に足した切り口。目立つバケットは検証済みの結果ではなく仮説。**

| 切り口 | バケット（出力の順） |
|---|---|
| `weekday` | 判断の JST の曜日: `mon` / `tue` / `wed` / `thu` / `fri` / `other`（土日） |
| `volatility_band` | `jev_decisions.regime_volatility_bps` の三分位: `low` / `mid` / `high` / `unknown`（値なし） |
| `turnover_band` | 判断の足の `turnover_5m` の三分位（同上） |
| `spread_band` | 判断の足の `spread_bps` の三分位（同上） |
| `sector` | `instruments.sector`。コホートの評価行数の多い上位 20 業種（`MaxSectorBuckets`。同数は名前順）は名前順のバケット、残りは `other`、記録なしは `unknown`（`other` / `unknown` は該当行があるときだけ） |

三分位: コホートの評価行のうち値のある行を、値の昇順（同値は判断 id の昇順）に並べ、k 件中 i 番目（0 起点）を `i×3÷k` の段（0=low, 1=mid, 2=high）に入れる。決定的で、値がない行は順位に入れず `unknown`。`edges` は mid と high の最初の値（値のある行が 3 件未満なら出ない）。**同値が境界をまたぐと同じ値が 2 つのバケットに入りうる**。境界はコホート・期間・フィルタが変わると動く（固定の水準ではない）。

## 応答の `data`

| キー | 内容 |
|---|---|
| `from` / `to` / `horizon_minutes` / `prompt_version` / `model_id` | 解決した期間（JST）・使ったホライズン・渡した絞り込み |
| `dimensions` | 返した切り口（固定の順） |
| `notice` | 研究用・採用基準ではない・並べ替えない・何も書かない、の固定文 |
| `exploratory_note` | exploratory の結果は仮説で確認ではない、の固定文 |
| `preregistration` | `date`（2026-11-15）・`issue`（#1089）・`dimensions`（事前登録の 4 つ）・`statement` |
| `decisions` | 読んだ判断の数（`jev_evaluation` と同じ） |
| `llm_cutoff_applied` | `metalabel.llm_cutoff` を適用したか。`false`（仮置き）のときは何も除外せず、`caveats` に出す |
| `label_assumptions` | `horizon_minutes`・`take_profit_bps`・`stop_loss_bps`・`barrier_cost_bps`（いまの設定。`jev_evaluation` と同じ付け直し） |
| `statistics` | `method`・`alpha`（0.05）・`power`（0.8）・`resamples`（2000）・`seed`（1073）・`correction`（Holm）・`min_bucket_rows`（30）・`min_bucket_days`（5） |
| `cohorts[]` | `(model_id, question_version)` ごと。最初の判断の時刻の順（`jev_evaluation` と同じ） |
| `definitions` | 母集団・各切り口・`low_sample`・`p_value`・Holm などの定義文（日本語） |
| `caveats[]` | 数字の限界（日本語） |

**`cohorts[]` の要素**: `model_id`・`question_version`・`decisions`（読んだ全判断）・`evaluated`（比較に使えた行）・`all`（そのコホート全体のバケット形式の要約。基準で、family に入らず `holm_adjusted_p` は `null`）・`families[]`。

**`families[]` の要素**: `family`（`preregistered` / `exploratory`。`dimensions` で片方の切り口を全部外すとその family は出ない）・`family_size`・`buckets`・`dimensions[]`。**`family_size`** は Holm の m = その family の**全切り口の全バケット**（`dimensions` で絞っても同じ）のうち `p_value` があるものの数。`buckets` は `p_value` のないものも含む数。

**`dimensions[]` の要素**: `dimension`・`rows`（= コホートの `evaluated`）・`edges`（三分位の切り口のみ）・`buckets[]`。

**バケット**（llm アームのコスト控除後 net bps。`evaldiag.Summarize` と同じ計算）:

| キー | 内容 |
|---|---|
| `bucket` | バケット名 |
| `n` / `days` | 件数 / JST の日数 |
| `mean_net_bps` | 平均（0 件は `null`） |
| `ci` | `lower` / `upper` / `level`。判断の JST 日を単位にした 95% のブートストラップ百分位区間（2 日未満は `null`） |
| `win_rate` | `net > 0` の件数 / `n` |
| `detectable_minimum_edge_bps` | いまの標準誤差で 0 と区別できる最小の平均（α=0.05 両側・検出力 0.8。2 日未満は `null`） |
| `p_value` | 「真の平均が 0」の両側 p 値（ブートストラップの標準誤差による正規近似。2 日未満・標準誤差 0 は `null`）。Holm の入力で、合否ではない |
| `holm_adjusted_p` | family（コホートごと・`preregistered` / `exploratory` ごと）の全バケットに `stats.Holm` を適用した p 値。`p_value` 以上で 1 以下。`p_value` が無ければ `null` |
| `low_sample` / `low_sample_reasons` | `n < 30`（`rows_below_min`）または `days < 5`（`days_below_min`）で `true`。除外せず family に入れたまま印だけ付ける |

## 定数

`MinBucketRows`=30・`MinBucketDays`=5・`MaxSectorBuckets`=20（`slicediag`）。`Alpha`=0.05・`Power`=0.8・`Resamples`=2000・`Seed`=1073・`MaxDecisions`・`MaxWindowRows`・`MaxCohorts`（`evaldiag`）。応答の `statistics` にも出る。

## 注意（`caveats` にも出る）

- 値は**いまの設定で付け直したラベル**（保存済みの `net_return_bps` ではない）。保存ラベルとの差は `jev_evaluation` の `stored_label`
- `low_sample` の印がないバケットでも、判断が同じ日・同じ銘柄・同じ相場に偏れば日クラスタの区間は楽観的になりうる
- 切り口どうしは独立ではない（同じ行が全切り口に 1 回ずつ入る）。Holm は family 内の検定の数だけを数えており、切り口の組み合わせ・出していない切り方は数えない。exploratory の補正後の p 値でも仮説のまま
- 三分位の境界はコホートの評価行から計算される。期間・フィルタが変わると同じ帯でも中身が変わる
- cutoff が仮置きの間は、学習データに含まれうる日も除外していない（`llm_cutoff_applied=false`）

## 出力の例（合成データ・抜粋）

`slicetests` の固定データ（`model-1` / `trader-v3`、2026-10-05〜07 の評価行 7 件。TP/SL 30・コスト 10 bps・10 分）の一部:

```json
{
  "from": "2026-10-05", "to": "2026-10-12", "horizon_minutes": 10,
  "dimensions": ["regime", "time_of_day", "direction", "confidence_band", "weekday", "volatility_band", "turnover_band", "spread_band", "sector"],
  "preregistration": {"date": "2026-11-15", "issue": "#1089", "dimensions": ["regime", "time_of_day", "direction", "confidence_band"]},
  "llm_cutoff_applied": false,
  "cohorts": [{
    "model_id": "model-1", "question_version": "trader-v3", "decisions": 8, "evaluated": 7,
    "all": {"bucket": "all", "n": 7, "days": 3, "mean_net_bps": -7.14, "low_sample": true, "low_sample_reasons": ["rows_below_min", "days_below_min"], "holm_adjusted_p": null},
    "families": [{
      "family": "preregistered", "family_size": 7, "buckets": 16,
      "dimensions": [{
        "dimension": "direction", "rows": 7,
        "buckets": [
          {"bucket": "LONG", "n": 4, "days": 3, "mean_net_bps": -5.0, "win_rate": 0.25, "low_sample": true, "low_sample_reasons": ["rows_below_min", "days_below_min"]},
          {"bucket": "SHORT", "n": 3, "days": 3, "mean_net_bps": -10.0, "win_rate": 0.333, "low_sample": true, "low_sample_reasons": ["rows_below_min", "days_below_min"]}
        ]
      }]
    }]
  }]
}
```

（`ci` / `p_value` などは省略。この固定データでは、p 値のあるバケットが少ない（多くが 1 日分か 2 日未満）ので、`family_size` は preregistered が 7（16 バケット中）、exploratory が 6（21 バケット中）。）

## 受け入れ条件

- `tools/list` に出て `readOnlyHint: true`。呼び出しで発行される SQL は SELECT だけで、DB の全表が呼び出しの前後で変わらない
- 切り口ごとのバケットの `n` の合計がコホートの `evaluated` に一致する。バケットの件数・平均・区間・勝率・最小の差・p 値が手計算（日クラスタのブートストラップ）と一致する
- `all` と件数・cutoff・コホートの分け方が `jev_evaluation` の llm と一致する
- `low_sample` の印と理由が境界（30 件・5 日）で正しい。Holm の補正後の値が family ごとに `stats.Holm` と一致し、`p_value` 以上で、`preregistered` と `exploratory` が別の family として別の `family_size` を持つ
- 未知の切り口・不正な引数は `invalid_argument`、期間・件数の上限超過は `limit_exceeded`（部分結果なし）
- 応答に `response_json` / `state_json` / `raw_data_json` の中身・秘密・許可外の設定値を含めない
- テスト: `internal/service/paperresultsmcp/slicetests`（HTTP の外から）

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規（切り口の事前登録） | issue #1089 |
