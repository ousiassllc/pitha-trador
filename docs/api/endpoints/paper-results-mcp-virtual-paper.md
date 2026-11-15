# API 仕様: Paper 実績 MCP の仮想 Paper ツール

`docs/api/endpoints/paper-results-mcp.md` の分割章（行数上限のため別ファイル）。issue #1087。共通の上限・トランスポート・エラー形式は `paper-results-mcp.md` に従う。設計は `docs/architecture/paper-results-mcp-virtual-paper.md`。

「仮想 Paper」は、本物の Paper と同じ判断・同じ相場に対して、方針・出口の設定の**差分**だけを変えたプロファイルを並べて記録し、比べる記録専用の機能。取引・Risk・発注・設定・資金には何も反映しない。プロファイルが 0 件（既定）のあいだは何も動かない。記録は本体の日次の保守タスク（16:00 JST 以降）が、終わった日ごとに再生して書くので、**結果は取引日の終わった後（最大で翌日）に出る**。基準になる設定は、その日を処理した時点で本体が使っている設定。

## 3. ツール

### `virtual_paper_profile_create`（書き込み）

プロファイルを 1 件作る。複製も兼ねる。書き込み許可がオフのときは `write_disabled`。

引数:

| キー | 内容 |
|---|---|
| `name` | 必須。1〜40 文字。停止したものも含めて重複不可。先頭・末尾の空白と制御文字は不可 |
| `params` | 任意。本物の Paper との差分（すべて任意。省略した項目は本物の Paper と同じ）。下の表 |
| `clone_from` | 任意。`profile_id`。そのプロファイルの `params` を写してから `params` の項目で上書きする |

`params`（バックテストラボと共通の `variant.Params`）:

| キー | 内容・範囲 |
|---|---|
| `entry_rule` | `meta`（本体と同じ）/ `legacy_confidence`（Jev の確信度） |
| `min_ev_bps` | meta 規則の最低期待値。0〜1000 |
| `min_entry_quality_long` / `_short` | `poor` / `fair` / `good` / `strong` / `exceptional` |
| `min_probability_long` / `_short` | legacy_confidence の最低確率。0 より大きく 1 以下 |
| `min_continuation_probability_long` / `_short` | 最低継続確率。0〜1 |
| `stop_loss_pct` / `take_profit_pct` / `trailing_stop_pct` | %。0 より大きく 100 以下 |
| `max_holding_minutes` | 最大保有時間（分）。0〜1440 |
| `slippage_bps` / `fee_bps` | 約定コスト（bps）。0〜200 |
| `max_trade_loss_pct` | 1 取引の最大損失（資金の %）。0.1〜3 |
| `max_position_per_symbol_pct` | 1 銘柄の最大ポジション（%）。0 より大きく 100 以下 |

応答の `data`: `profile`（`profile_id` / `name` / `status`（`active`）/ `params`（正規形の差分）/ `first_day`（記録を始める JST の日。作成した日の翌日）/ `created_at` / `stopped_at`（null）/ `cloned_from`）。

エラー: 範囲外の値・名前の不備・名前の重複は `invalid_value`（`error.data.detail` に `項目名: 理由`。値は載せない）、未知の引数・存在しない `clone_from` は `invalid_argument`、上限（同時に有効 5 件・合計 20 件）超過は `limit_exceeded`（detail に上限）、`write_disabled`。

### `virtual_paper_profile_stop`（書き込み）

引数 `profile_id`（必須）。プロファイルを止める。以後の日は記録しない（止めた時点で未処理の日は、終わっていても記録しない）。記録済みの取引・日は残り、比較にも使える。止めたあとは同時に有効な 5 件に数えない。

応答の `data`: `profile`（`status` が `stopped`、`stopped_at` つき）、`stopped`（今回止めたら true）、`already_stopped`（もう止まっていたら true。失敗にはしない）。存在しない `profile_id` は `invalid_argument`。`write_disabled`。

### `virtual_paper_compare`（読み取り専用）

プロファイルを、同じ期間・同じ判断の範囲で比べる。

引数: `from` / `to`（他のツールと同じ JST の期間。省略は直近 30 日。最大 366 日）、`profile_ids`（任意。2〜5 件の `profile_id`。先頭が比較の基準。省略は期間にデータのあるすべてのプロファイル。6 件以上あると `limit_exceeded`）。`profile_ids` が 1 件・重複・存在しない ID・6 件以上のときは `invalid_argument`。省略してデータが無いときはエラーにせず `profiles` を空配列で返す。

比べる日は、**全員が記録を終えた日の共通部分**。誰かが欠く日はどのプロファイルの数字にも入れない。

応答の `data`:

| キー | 内容 |
|---|---|
| `from` / `to` | 比べた JST の日付の範囲（`to` は含まない） |
| `days` | 比べた日（全員が記録済み）の昇順 |
| `excluded_days[]` | 外した日。`day`、`missing_profile_ids`（その日の記録が無いプロファイル） |
| `profiles[]` | 指定順（省略時は ID 順）。下の表 |
| `caution` | 固定文。記録専用・標本が小さい間は偶然・多重比較・何も反映しない |

`profiles[]` の要素:

| キー | 内容 |
|---|---|
| `profile_id` / `name` / `status` / `params` | プロファイル（`params` は正規形の差分） |
| `trade_count` | 共通の日に、出口の条件で閉じた取引の件数 |
| `unfinished` | 共通の日に、データが尽きて強制的に閉じた取引（`exit_reason = data_ended`）の件数。指標には入れない |
| `win_rate` | 純損益（コスト控除後）が 0 より大きい取引の割合 |
| `expectancy_pct` | 1 取引あたりの純損益（スリッページ・手数料を引いた後。%）の平均。`n` / `value` / `lo` / `hi` / `has_ci` / `defined` / `reliable`。区間は日ごとのまとまりで計算した 95%（日数が 2 日未満だと `has_ci` が false） |
| `total_pnl_jpy` | 記録した円建ての損益（純リターン × 数量 × 約定価格 / 100）の合計 |
| `max_drawdown_pct` | 取引を決済順に並べた累積純損益（%）の、山からの最大の落ち込み |
| `vs_first` | 先頭以外のプロファイルのみ。`against_profile_id`、`daily_net_return_pct_diff`（共通の日ごとの純損益の合計の、先頭との差の平均と、その日単位の 95% 区間。取引の無い日は 0）、`trade_count_diff` |

1 プロファイルの期間内の取引が 20,000 件を超えると `limit_exceeded`（期間を狭める）。約定は本体の約定モデル（`fillmodel`）の仮定で、数量は再生したリスクエンジンが決めた値。
