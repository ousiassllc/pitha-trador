# ER / データモデル: テーブル定義（仮想 Paper）

`docs/architecture/er.md` から分割した章。対象: `virtual_paper_profiles` / `virtual_paper_days` / `virtual_paper_trades`（マイグレーション `000066`。issue #1087）。型・規約は `er.md` §型・規約に従う（時刻は UTC の RFC3339・小数 9 桁固定）。

## 位置づけ

本物の Paper と同じ判断・同じ相場に対し、設定の差分だけを変えた「仮想 Paper」のプロファイルの記録。**研究専用**で、取引には使わない（Policy・Risk・発注・Paper ガード・Live・Jev・自己改善は読まない。`internal/repository/virtualpaper/isolation_test.go` が固定）。書き込むのは日次の記録部品（`internal/service/virtualpaper`、`internal/bootstrap/virtualpaperjob` が保守タスクとして動かす）と、MCP のプロファイルの作成・停止（`virtual_paper_profiles` の INSERT / UPDATE だけ）。`paper_orders`・`positions`・資金・`runtime_settings`・`market_snapshots`・`jev_decisions` には何も書かない。既存の表に列・制約・トリガは足さない。外部キーは自表のプロファイルにだけ張る（`market_snapshots` の保持期間による削除を止めない）。

記録は日次の保守タスクが、終わった JST の日ごとに既存のバックテストの再生（`ShadowBacktestPortfolioTrades`。約定の仮定は `fillmodel`）で書く。約定・相場の処理・判断の経路には一切つながらない。外部キーが自表だけなので、`market_snapshots` / `jev_decisions` の削除（保持期間 90 日）を止めたり連鎖させたりしない。

## virtual_paper_profiles

プロファイル 1 件 1 行。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| id | integer | PK（AUTOINCREMENT） | |
| name | text | NOT NULL, UNIQUE, CHECK (長さ 1〜40) | 名前。停止したものも含めて重複不可 |
| params_json | text | NOT NULL, CHECK (長さ 2〜2000) | `variant.Params` の正規形（`Params.Canonical()`）。差分だけ。`{}` は本物の Paper と同じ |
| status | text | NOT NULL, DEFAULT `active`, CHECK (`active` / `stopped`) | `active` は記録する、`stopped` は記録しない（記録済みのデータは残す） |
| created_at | text | NOT NULL | 作成時刻 |
| first_day | text | NOT NULL, CHECK (長さ 10) | 記録を始める JST の日（作成した日の翌日）。作成した日は途中なので入れず、この日以降の日だけを再生する |
| stopped_at | text | NULL 可 | 停止時刻。`status = 'active'` のときだけ NULL（CHECK） |
| cloned_from | integer | NULL 可（外部キーなし） | 複製元のプロファイル ID。複製元は別の行の事実の記録でしかないので外部キーにしない |
| actor | text | NOT NULL, CHECK (`mcp`) | 作成者（MCP のみ） |
| client | text | NOT NULL, DEFAULT `''` | 接続元ホストの IP。ヘッダの値・API キーは入れない |

索引: `virtual_paper_profiles_status_idx (status, id)`。上限（同時に有効 5 件・合計 20 件）は書き込みのコードで守る。

## virtual_paper_days

(プロファイル, JST の日) ごとの「再生を終えた」印。取引の行と同じトランザクションで書く。印の無い日は次の実行がやり直す。主キーは `(profile_id, day)`（WITHOUT ROWID）。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| profile_id | integer | NOT NULL, FK → virtual_paper_profiles.id | |
| day | text | NOT NULL, CHECK (長さ 10) | JST の日付 |
| status | text | NOT NULL, CHECK (`done` / `empty` / `failed`) | `done` 再生した、`empty` その日の足が無い（休場・未収集）、`failed` 再生が失敗（自動ではやり直さない） |
| bars | integer | NOT NULL, DEFAULT 0, CHECK (>= 0) | 再生した足の数（ウォームアップを除く） |
| decisions_seen | integer | NOT NULL, DEFAULT 0, CHECK (>= 0) | エントリーの評価に使った新しい Jev の判断の数（`RunConfig.OnEntryEvaluation` の回数）。削除後も残る分母 |
| trades | integer | NOT NULL, DEFAULT 0, CHECK (>= 0) | 記録した取引の数（unfinished を含む） |
| unfinished | integer | NOT NULL, DEFAULT 0, CHECK (0〜trades) | データが尽きて強制的に閉じた取引（`exit_reason = data_ended`）の数。比較の指標から外す |
| error | text | NOT NULL, DEFAULT `''`, CHECK (長さ <= 200) | `failed` のときの理由（先頭 200 文字。値・秘密は入れない） |
| completed_at | text | NOT NULL | 印を付けた時刻 |

比較（`virtual_paper_compare`）は `status = 'done'` の日だけを使う。

## virtual_paper_trades

仮想のエントリー・イグジット 1 件 1 行。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| id | integer | PK（AUTOINCREMENT） | |
| profile_id | integer | NOT NULL, FK → virtual_paper_profiles.id | |
| day | text | NOT NULL, CHECK (長さ 10) | 再生した JST の日（エントリーの日） |
| instrument_id / symbol | integer / varchar(10) | NOT NULL（外部キーなし） | 銘柄 |
| direction | text | NOT NULL, CHECK (`LONG` / `SHORT`) | 向き |
| entry_at / exit_at | text | NOT NULL | 約定の時刻 |
| entry_price / exit_price | numeric | NOT NULL, CHECK (`> 0`) | `fillmodel` が板・スリッページ・呼値・寄り引けを反映した約定価格 |
| exit_reason | text | NOT NULL, CHECK (長さ 1〜60) | `domain.ExitReason*` と同じ名前。`data_ended` はデータが尽きて閉じたもの |
| quantity | integer | NOT NULL, CHECK (>= 0) | 再生したリスクエンジンが決めた数量（0 はリスクエンジン無しの再生） |
| gross_return_pct / slippage_return_pct / fee_return_pct / net_return_pct | numeric | NOT NULL | 方向調整済みのリターン（%。1.0 = +1%）。net がコスト（スリッページ・手数料）控除後で、比較はこれを使う |
| pnl_jpy | numeric | NOT NULL | `net_return_pct × quantity × entry_price / 100`。記録時に計算した円建ての損益の見積り |
| created_at | text | NOT NULL | 記録時刻 |

制約: `UNIQUE (profile_id, instrument_id, entry_at)`（同じ日を再生し直しても二重にならない。書き込みは `ON CONFLICT DO NOTHING`）。索引: `virtual_paper_trades_profile_day_idx (profile_id, day)`。
