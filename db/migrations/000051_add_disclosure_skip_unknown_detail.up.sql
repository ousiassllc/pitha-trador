-- disclosure_skip_records に、「不明（unknown）」の理由の内訳を足す（issue #1053、関連 #935）。
-- no_fetch / incomplete_fetch の 2 分類だけでは次の打ち手が選べないため、確定時に細かい理由を記録に残す。
-- 取得の記録（disclosure_fetches）は 7 日で消えるので、後から突き合わせて分類し直すことはできない。
-- 確定の時点で理由を決めて、この列に保存する。
--
-- **NULL 可。NULL は「内訳なし」**（この列ができる前の行、または unknown 以外の行）。既存行は UPDATE せず
-- （後追いの分類をしない）、MCP は NULL を unclassified として数える。
--
-- unknown_detail  verdict='unknown' のときだけ入る。
--   unknown_reason='no_fetch' の内訳:
--     fetch_failed             判断の後に取得を試みたが失敗した（失敗は覆いに数えない）
--     lunch_break              判断の 5 分後（FetchLag）が昼休み（11:30–12:30）で、次の後場の取得でも覆えなかった
--     after_close              判断の 5 分後が大引け後（立会時間外）で、次の立会の取得でも覆えなかった
--     within_fetch_lag         判断の直後 5 分以内の成功取得しか無い（FetchLag に届かない）
--     dropped_from_candidates  判断より前の成功取得しか無い（News Ingest の対象から外れ、後追いも届かなかった）
--     no_fetch_at_all          判断の前後とも、その銘柄の成功取得が無い
--   unknown_reason='incomplete_fetch' の内訳:
--     undated_items            開示時刻を読めなかった（または捨てられた）項目があった
--     item_limit               件数上限で切られ、窓の始まりまで届かなかった
ALTER TABLE disclosure_skip_records ADD COLUMN unknown_detail TEXT CHECK (unknown_detail IS NULL OR (verdict = 'unknown' AND unknown_detail IN (
    'fetch_failed', 'lunch_break', 'after_close', 'within_fetch_lag', 'dropped_from_candidates', 'no_fetch_at_all',
    'undated_items', 'item_limit')));
