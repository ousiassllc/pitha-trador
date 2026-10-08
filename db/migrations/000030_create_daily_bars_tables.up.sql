-- daily_bars: 立花 e支店APIの夜間バッチ（issue #729、親 #726）が取り込む日足
-- （docs/architecture/er/tables-market.md §daily_bars）。1銘柄1立会日1行で、
-- 自己利用のローカル保存に限る（外部へ出す経路は作らない、#720）。
-- instruments には紐付けない: スクリーニングの母集団は instruments（監視対象）より広い全銘柄。
-- open/high/low/close/volume は無調整値（pDOP 等）、adj_* は株式分割換算係数で
-- 調整した値（pDOPxK 等）。売買代金は応答に無いため持たない。
CREATE TABLE daily_bars (
    symbol VARCHAR(10) NOT NULL,
    trade_date TEXT NOT NULL CHECK (trade_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    open NUMERIC NOT NULL,
    high NUMERIC NOT NULL,
    low NUMERIC NOT NULL,
    close NUMERIC NOT NULL,
    volume NUMERIC NOT NULL,
    adj_open NUMERIC NOT NULL,
    adj_high NUMERIC NOT NULL,
    adj_low NUMERIC NOT NULL,
    adj_close NUMERIC NOT NULL,
    adj_volume NUMERIC NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (symbol, trade_date)
) WITHOUT ROWID;

-- daily_bar_runs: 夜間バッチの1夜1行の実行記録。再起動後の二重実行の防止と、
-- 中断した夜の再開位置（cursor）、取得に失敗した夜の判定（後続の監視リスト確定が読む）に使う。
CREATE TABLE daily_bar_runs (
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
