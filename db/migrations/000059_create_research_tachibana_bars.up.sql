-- 立花の研究用の日足収集（issue #1082）の記録。取引には使わない（docs/architecture/er/tables-research.md）。
-- 状態は認証情報を含めず、未設定のときは not_configured とだけ記録する。daily_bar_runs とは混ぜない。
CREATE TABLE research_tachibana_status (
    component TEXT PRIMARY KEY CHECK (component IN ('bars', 'quotes')),
    state TEXT NOT NULL CHECK (state IN ('disabled', 'not_configured', 'broker_is_tachibana', 'waiting', 'running', 'ok', 'error')),
    detail TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);
CREATE TABLE research_daily_bar_runs (
    run_date TEXT PRIMARY KEY CHECK (run_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    started_at TEXT NOT NULL,
    finished_at TEXT,
    symbols INTEGER NOT NULL DEFAULT 0,
    requests INTEGER NOT NULL DEFAULT 0,
    saved_bars INTEGER NOT NULL DEFAULT 0,
    failed INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    cursor TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT ''
);
-- 銘柄ごとの進み具合。last_night の夜に試みた銘柄は同じ夜に取り直さない。first_ok_night NULL = 未埋め戻し。
CREATE TABLE research_daily_bar_progress (
    symbol VARCHAR(10) PRIMARY KEY,
    last_night TEXT NOT NULL CHECK (last_night GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    last_ok INTEGER NOT NULL CHECK (last_ok IN (0, 1)),
    first_ok_night TEXT CHECK (first_ok_night IS NULL OR first_ok_night GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    bars_saved INTEGER NOT NULL DEFAULT 0,
    failures INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);
CREATE INDEX research_daily_bar_progress_night_idx ON research_daily_bar_progress (last_night);
