-- watch_list_runtime_days: 実行時の監視リスト（メモリ上の rankingwatch.Watchlist。kabu のランキング駆動）の
-- 大きさを、立会日ごと・取得元ごとに 1 行で残す（issue #920。#914 の watch_list_not_applicable の後続）。
--
-- watch_lists / watch_list_entries は立花アダプタ（tachibanawatch.Decider）が「翌営業日分として引け後に確定した
-- リスト」を銘柄つきで書く表で、意味が違う（確定した計画 vs 立会中に実際に動いていたリストの大きさ）。
-- そのため kabu の行を watch_lists へ混ぜず、別の小さな表にした（watch_lists の「立花だけが書く」前提と
-- watch_list_empty の意味を変えない）。
--
-- 持つのは件数と取得元だけで、銘柄コード・価格・理由は持たない。書くのはアプリの
-- internal/service/watchlistsize.Recorder（立会中 1 分ごとに Watchlist.Len() を読むだけ。ランキング取得・Scout 投入・
-- 注文には触れない。書き込みに失敗してもログだけで取引は止まらない）で、読むのは Paper 実績 MCP
-- （session_diagnostics の days[].watch_list）だけ。取引・リスク・ポリシー・Jev は読まない。
-- 1 日 1 行で小さいため保持期間の削除対象にしない（session_diagnostics_daily と同じ扱い）。
--
--   session_date  立会日（JST の YYYY-MM-DD）。
--   source        リストの取得元。'ranking' = ブローカーのランキング駆動（kabu。保有銘柄・観測銘柄の固定枠を含む）。
--                 kabu に固定リストの経路は無いので、いまは 'ranking' だけ。
--   entries       その日の立会中の最後の観測の件数。
--   peak_entries  その日の立会中の観測の最大件数。起動直後の最初のサイクル前の 0 件の観測と区別するため。
--                 0 は「その日の観測がすべて 0 件」の意味。
--   samples       立会中に観測した回数（1 分に 1 回。少ない日はアプリが止まっていた手がかり）。
--   recorded_at   最後に書いた時刻（RFC3339 UTC）。
CREATE TABLE watch_list_runtime_days (
    session_date TEXT NOT NULL CHECK (session_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    source TEXT NOT NULL CHECK (source IN ('ranking')),
    entries INTEGER NOT NULL CHECK (entries >= 0),
    peak_entries INTEGER NOT NULL CHECK (peak_entries >= entries),
    samples INTEGER NOT NULL CHECK (samples >= 1),
    recorded_at TEXT NOT NULL,
    PRIMARY KEY (session_date, source)
) WITHOUT ROWID;
