-- jobs: 自前ワーカーキュー（River代替）。market-data/feature-calc/jev-scout/
-- jev-trader/risk-check/paper-execution/outcome-labeling/analyticsの8キューを
-- このテーブルとinternal/service/schedulerのGoワーカープールで実現する
-- （docs/architecture/er.md §jobs）
CREATE TABLE jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    queue VARCHAR(30) NOT NULL,
    payload_json TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    scheduled_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    last_error TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX jobs_queue_status_scheduled_idx ON jobs (queue, status, scheduled_at);
