-- MCP から起動した研究用バックテスト 1 回分の記録（issue #1086、研究専用）。列の意味: docs/architecture/er/tables-backtest.md
CREATE TABLE backtest_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    owner TEXT NOT NULL, client TEXT NOT NULL,
    created_at TEXT NOT NULL, started_at TEXT, finished_at TEXT,
    period_from TEXT NOT NULL CHECK (length(period_from) = 10), period_to TEXT NOT NULL CHECK (length(period_to) = 10),
    first_snapshot_at TEXT, last_snapshot_at TEXT,
    replay_mode TEXT NOT NULL CHECK (replay_mode IN ('paper', 'live')), code_version TEXT NOT NULL,
    explored_count INTEGER NOT NULL CHECK (explored_count >= 1 AND explored_count <= 12),
    request_json TEXT NOT NULL, result_json TEXT,
    error_code TEXT CHECK (error_code IS NULL OR error_code IN ('timed_out', 'interrupted', 'unavailable', 'internal')),
    CHECK ((status IN ('succeeded', 'failed')) = (finished_at IS NOT NULL)),
    CHECK ((status = 'succeeded') = (result_json IS NOT NULL)),
    CHECK ((status = 'failed') = (error_code IS NOT NULL)),
    CHECK (status IN ('queued', 'failed') OR started_at IS NOT NULL)
);
CREATE INDEX backtest_runs_status_idx ON backtest_runs (status, id);
