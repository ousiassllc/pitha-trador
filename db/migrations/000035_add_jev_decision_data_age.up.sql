-- 判断時点での市場データの古さ（ミリ秒）を jev_decisions に保存する（issue #754、NULL可）。
-- Jev に渡す state の data_age_ms と同じ値で、後から判断の鮮度と成績の関係を分析するために使う。
ALTER TABLE jev_decisions ADD COLUMN data_age_ms INTEGER;
