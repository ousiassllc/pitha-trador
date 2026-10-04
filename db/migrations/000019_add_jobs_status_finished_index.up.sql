-- jobs: (queue, status, finished_at) 索引。QueueCounts の直近 failed 件数
-- （Activity Log、issue #392）と ListOpenOrFinishedSince の直近完了行
-- （Jev Scout 間引き、issue #395）が、保持期間内の完了行（最大 7 日分で数千万行）を
-- 全走査せず finished_at の範囲検索で引けるようにする。pending/running は既存の
-- jobs_queue_status_scheduled_idx で引く。
CREATE INDEX jobs_queue_status_finished_idx ON jobs (queue, status, finished_at);
