-- retention の Purge（internal/service/retention、issue #533）が期限切れ行を
-- 範囲検索で引けるようにする索引。
--
-- market_snapshots(timestamp): purgeSnapshotBatch の
--   SELECT id FROM market_snapshots WHERE timestamp < ? ORDER BY timestamp, id LIMIT ?
-- 用。既存の (instrument_id, timestamp) / (symbol, timestamp) では timestamp 単独の
-- 範囲を引けず、期限切れ行が LIMIT 件に満たない最終バッチ（毎日の Purge で必ず発生）が
-- 保持期間分の全行を走査したまま BEGIN IMMEDIATE の書き込みロックを握っていた。
-- 索引の並び（timestamp, 暗黙の rowid=id）は ORDER BY と一致し、整列も発生しない。
CREATE INDEX market_snapshots_timestamp_idx ON market_snapshots (timestamp);

-- jobs(status, finished_at): purgeJobs の
--   SELECT id FROM jobs WHERE status = ? AND finished_at < ? LIMIT ?
-- 用。Purge は queue を条件に含めないため (queue, status, finished_at) 索引の
-- 先頭列を指定できず、jobs の全走査になっていた。
CREATE INDEX jobs_status_finished_idx ON jobs (status, finished_at);
