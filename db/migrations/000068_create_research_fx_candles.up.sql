-- 研究用の為替 1 分足（記録専用。取引には使わない。列の意味と制約は docs/architecture/er/tables-research.md）。BID と ASK は open_time で 1 行にまとめる（片側だけの分は他方が NULL）。
CREATE TABLE research_fx_candles (
    symbol VARCHAR(7) NOT NULL CHECK (symbol GLOB '[A-Z][A-Z][A-Z]_[A-Z][A-Z][A-Z]'),
    open_time TEXT NOT NULL,
    trading_day TEXT NOT NULL CHECK (length(trading_day) = 10),
    bid_open NUMERIC CHECK (bid_open > 0), bid_high NUMERIC CHECK (bid_high > 0), bid_low NUMERIC CHECK (bid_low > 0), bid_close NUMERIC CHECK (bid_close > 0),
    ask_open NUMERIC CHECK (ask_open > 0), ask_high NUMERIC CHECK (ask_high > 0), ask_low NUMERIC CHECK (ask_low > 0), ask_close NUMERIC CHECK (ask_close > 0),
    recorded_at TEXT NOT NULL,
    PRIMARY KEY (symbol, open_time),
    CHECK ((bid_open IS NULL) = (bid_high IS NULL) AND (bid_open IS NULL) = (bid_low IS NULL) AND (bid_open IS NULL) = (bid_close IS NULL)),
    CHECK ((ask_open IS NULL) = (ask_high IS NULL) AND (ask_open IS NULL) = (ask_low IS NULL) AND (ask_open IS NULL) = (ask_close IS NULL)),
    CHECK (bid_open IS NOT NULL OR ask_open IS NOT NULL)
) WITHOUT ROWID;
CREATE INDEX research_fx_candles_day_idx ON research_fx_candles (symbol, trading_day);
-- 取引日ごとの「取得を終えた」印。'empty' は GMO が答えたが足が 0 本の日（休場。欠けではない）。印の無い日は欠け。
CREATE TABLE research_fx_days (
    symbol VARCHAR(7) NOT NULL CHECK (symbol GLOB '[A-Z][A-Z][A-Z]_[A-Z][A-Z][A-Z]'),
    trading_day TEXT NOT NULL CHECK (length(trading_day) = 10),
    status TEXT NOT NULL CHECK (status IN ('recorded', 'empty')),
    candle_count INTEGER NOT NULL CHECK (candle_count >= 0), fetched_at TEXT NOT NULL,
    PRIMARY KEY (symbol, trading_day),
    CHECK ((status = 'empty') = (candle_count = 0))
) WITHOUT ROWID;
