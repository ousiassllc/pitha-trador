# ER / データモデル: テーブル定義（実行時の監視リストの日次の大きさ）

`docs/architecture/er.md` から分割した章。対象: `watch_list_runtime_days`（マイグレーション `000056`。issue #920）。

## watch_list_runtime_days

kabu のランキング駆動の監視リスト（メモリ上の`rankingwatch.Watchlist`。FR-SCHED-9）が立会中に何銘柄だったかを、**件数と取得元だけ**で立会日ごとに残す（マイグレーション000056）。`watch_lists`（立花が翌営業日分として確定した銘柄つきの計画）とは意味が違うため、別の表にした（`watch_lists`の「立花だけが書く」前提と`watch_list_empty`の意味は変えない）。書くのはアプリの`internal/service/watchlistsize.Recorder`だけ（立会中に1分ごと、`Watchlist.Len()`を読むだけ。ランキング取得・Scout投入・ブローカー選択・監視リスト生成・注文には触れない。書き込みの失敗はログだけで取引を止めない）。読むのはPaper実績MCPの`session_diagnostics`だけで、取引・Risk・Policy・Jevは読まない（`watchlistsize/isolation_test.go`が固定する）。銘柄コード・価格は持たない。1日1行で小さいため保持期間の削除対象にしない（`session_diagnostics_daily`と同じ）。`WITHOUT ROWID`。同じ`(session_date, source)`はその場で更新する（`entries`は最新、`peak_entries`は最大、`samples`は加算）。立会外（昼休みを含む）は観測しない。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| session_date | text | PK（`source`と複合）, CHECK `YYYY-MM-DD` | 立会日（JST） |
| source | text | PK, NOT NULL, CHECK IN ('ranking') | 取得元。`ranking`＝ブローカーのランキング駆動（保有・観測銘柄の固定枠を含む）。kabuに固定リストの経路は無いのでいまは`ranking`だけ |
| entries | integer | NOT NULL, CHECK >= 0 | その日の立会中の最後の観測の件数 |
| peak_entries | integer | NOT NULL, CHECK >= `entries` | その日の立会中の最大件数。0は「立会中の観測がすべて0件」（起動直後の最初のサイクル前の0件はピークで吸収される） |
| samples / recorded_at | integer / text | NOT NULL（`samples`は CHECK >= 1） | 立会中の観測回数（1分に1回。少ない日はアプリが止まっていた手がかり）／最後に書いた時刻（UTC） |
