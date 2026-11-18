-- 仮想 Paper（issue #1087、研究専用）。列の意味・制約の理由: docs/architecture/er/tables-virtual-paper.md
CREATE TABLE virtual_paper_profiles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 40),
    params_json TEXT NOT NULL CHECK (length(params_json) BETWEEN 2 AND 2000),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'stopped')),
    created_at TEXT NOT NULL, first_day TEXT NOT NULL CHECK (length(first_day) = 10), stopped_at TEXT, cloned_from INTEGER,
    actor TEXT NOT NULL CHECK (actor IN ('mcp')), client TEXT NOT NULL DEFAULT '',
    CHECK ((status = 'active') = (stopped_at IS NULL))
);
CREATE INDEX virtual_paper_profiles_status_idx ON virtual_paper_profiles (status, id);
CREATE TABLE virtual_paper_days (
    profile_id INTEGER NOT NULL REFERENCES virtual_paper_profiles (id),
    day TEXT NOT NULL CHECK (length(day) = 10),
    status TEXT NOT NULL CHECK (status IN ('done', 'empty', 'failed')),
    bars INTEGER NOT NULL DEFAULT 0 CHECK (bars >= 0), decisions_seen INTEGER NOT NULL DEFAULT 0 CHECK (decisions_seen >= 0),
    trades INTEGER NOT NULL DEFAULT 0 CHECK (trades >= 0),
    unfinished INTEGER NOT NULL DEFAULT 0 CHECK (unfinished >= 0 AND unfinished <= trades),
    error TEXT NOT NULL DEFAULT '' CHECK (length(error) <= 200), completed_at TEXT NOT NULL,
    PRIMARY KEY (profile_id, day)
) WITHOUT ROWID;
CREATE TABLE virtual_paper_trades (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id INTEGER NOT NULL REFERENCES virtual_paper_profiles (id),
    day TEXT NOT NULL CHECK (length(day) = 10),
    instrument_id INTEGER NOT NULL, symbol VARCHAR(10) NOT NULL,
    direction TEXT NOT NULL CHECK (direction IN ('LONG', 'SHORT')),
    entry_at TEXT NOT NULL, entry_price NUMERIC NOT NULL CHECK (entry_price > 0),
    exit_at TEXT NOT NULL, exit_price NUMERIC NOT NULL CHECK (exit_price > 0),
    exit_reason TEXT NOT NULL CHECK (length(exit_reason) BETWEEN 1 AND 60),
    quantity INTEGER NOT NULL CHECK (quantity >= 0),
    gross_return_pct NUMERIC NOT NULL, slippage_return_pct NUMERIC NOT NULL, fee_return_pct NUMERIC NOT NULL,
    net_return_pct NUMERIC NOT NULL, pnl_jpy NUMERIC NOT NULL, created_at TEXT NOT NULL,
    UNIQUE (profile_id, instrument_id, entry_at)
);
CREATE INDEX virtual_paper_trades_profile_day_idx ON virtual_paper_trades (profile_id, day);
