# ER / データモデル: テーブル定義（研究用の為替 1 分足）

`docs/architecture/er.md` から分割した章（研究用の記録の表は `docs/architecture/er/tables-research.md` の続き）。対象: `research_fx_candles` / `research_fx_days`（マイグレーション `000068`。issue #1084 の USD/JPY の部分）。設計は `docs/architecture/paper-results-mcp-research-fx.md`、読み取りツールは `docs/api/endpoints/paper-results-mcp-research-fx.md`（`fx_series_summary`）と `jev_evaluation` の `fx_symbol`。実装は `internal/repository/researchfx`（表）・`internal/service/researchfx`（記録部品）・`internal/bootstrap/researchfxjob`（日次タスク）・`internal/service/fx/gmo`（既存の読み取り専用アダプタ）。日経平均・TOPIX など指数の表は別の章（`er/research-tools/tables-research-index.md`）。

## 位置づけ（為替）

判断の良し悪しを地合いで切り分けるための為替の系列を、取引とは無関係に残す**研究専用**の記録。GMOコイン 外国為替FX の公開 API（認証なし）の 1 分足を既存のアダプタで取る。**既定オフ**（`runtime_settings` の `research.fx.enabled`。オフの間は外部への要求も書き込みもしない）。取引には使わない: Feature Engine・Policy・Risk・発注・Paper ガード・Live・Jev のプロンプト・RAG・メタモデルの学習・自己改善は読まない（`internal/repository/researchfx/isolation_test.go` が、参照できるファイルを記録部品・日次タスク・設定・読み取り専用の MCP ツールに限って固定し、記録のパッケージがブローカー・kabu・立花のクライアントに依存しないことも固定する）。既存の表には列も制約もトリガも足さない。値は研究用にのみ使い、再配布しない（GMOコインの約款）。

## research_fx_candles

1 行は (通貨ペア, 1 分足の開始時刻)。BID の足と ASK の足は GMO から別々に取るので、開始時刻で 1 行にまとめる。`WITHOUT ROWID`。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| symbol | varchar(7) | PK（1 列目）, CHECK (`GLOB '[A-Z][A-Z][A-Z]_[A-Z][A-Z][A-Z]'`) | GMO の通貨ペア名（`USD_JPY` など） |
| open_time | text | PK（2 列目） | 足の開始時刻（UTC・固定 9 桁） |
| trading_day | text | NOT NULL, CHECK (長さ 10) | GMO の取引日（**06:00 JST 切り替え**。その日の 06:00 JST から翌 06:00 JST）の日付 `YYYY-MM-DD`。`klines` の `date` として取得した日をそのまま入れる導出列 |
| bid_open / bid_high / bid_low / bid_close | numeric | NULL 可, CHECK (`> 0`) | 売値（BID）の足。4 列とも NULL か 4 列とも非 NULL（CHECK） |
| ask_open / ask_high / ask_low / ask_close | numeric | NULL 可, CHECK (`> 0`) | 買値（ASK）の足。同上 |
| recorded_at | text | NOT NULL | 記録した時刻（UTC） |

制約・索引: `PRIMARY KEY (symbol, open_time)`（同じ足を再度取っても行は増えない。記録は `INSERT ... ON CONFLICT DO NOTHING`）。BID・ASK のどちらかは非 NULL（CHECK）。片側にしか足が無い分は、もう片側の 4 列が NULL。`(symbol, trading_day)`（日ごとの数え直しと期間の絞り込み）。出来高・板は GMO の公開 API が返さないので列が無い。

## research_fx_days

取引日ごとの「取得を終えた」印。記録部品は、その日の BID・ASK の両方を取り終え、足を書いた**同じトランザクション**で 1 行を足す（足だけがあって印が無い日は残らない）。為替は 24 時間だが、土日・休日の取引日は足が無い。その日を「市場休み」と「まだ取っていない（欠け）」に分けるための表。`WITHOUT ROWID`。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| symbol / trading_day | varchar(7) / text | PK（2 列）。symbol は上と同じ CHECK、trading_day は CHECK (長さ 10) | 通貨ペアと取引日 |
| status | text | NOT NULL, CHECK (`recorded` / `empty`) | `recorded` = その日に足が 1 本以上あった。`empty` = GMO が答えたが BID・ASK とも足が 0 本だった（**市場休み。欠けではない**） |
| candle_count | integer | NOT NULL, CHECK (`>= 0`)、`(status = 'empty') = (candle_count = 0)` | 印を付けた時点のその日の行数（`empty` は 0） |
| fetched_at | text | NOT NULL | 取得した時刻（UTC） |

### 欠けの定義

- 取得期間（直近 7 日。完了した取引日だけ）の中で、**印の無い日**は「まだ取っていない・取得に失敗した」＝欠け。失敗した日は印を残さず、次の実行がやり直す。
- `empty` の日は市場休み（欠けではない）。
- `recorded` の日の、最初の足と最後の足のあいだで足が 1 本も無い分の連続は「日の中の欠け」（`fx_series_summary` の `interior_gaps`）。

## 保持と量（為替）

この記録には削除（保持期間）を設けていない。1 ペアで 1 日 1440 本ぐらい（土日の取引日は 0）、1 行は主キー込みで約 150 バイト [INFERENCE]。1 ペア・1 年で約 52 万行・約 80MB。通貨ペアは最大 5 件。
