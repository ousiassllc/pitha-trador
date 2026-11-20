-- 研究用の指数（日経平均・TOPIX など）の 1 分値（記録専用。取引には使わない。列の意味と制約は docs/architecture/er/research-tools/tables-research-index.md）。
-- 値は立花の日中の時価の記録（research_quote_samples、#1083）から取り、新しい外部要求は無い。symbol は立花の指数コード（101 = 日経平均、151 = TOPIX など）。
CREATE TABLE research_index_minutes (
    symbol VARCHAR(8) NOT NULL CHECK (length(symbol) BETWEEN 1 AND 8 AND symbol NOT GLOB '*[^0-9A-Za-z]*'),
    minute_time TEXT NOT NULL,
    trading_day TEXT NOT NULL CHECK (length(trading_day) = 10),
    price NUMERIC NOT NULL CHECK (price > 0),
    via TEXT NOT NULL CHECK (via IN ('event', 'rest')),
    recorded_at TEXT NOT NULL,
    PRIMARY KEY (symbol, minute_time)
) WITHOUT ROWID;
CREATE INDEX research_index_minutes_day_idx ON research_index_minutes (symbol, trading_day);
-- 立会日ごとの「確かめた」印。'unavailable' は立花の記録に指数の値が 1 件も無かった日（取れていない。市場休みではない）。印の無い立会日は欠け。
CREATE TABLE research_index_days (
    symbol VARCHAR(8) NOT NULL CHECK (length(symbol) BETWEEN 1 AND 8 AND symbol NOT GLOB '*[^0-9A-Za-z]*'),
    trading_day TEXT NOT NULL CHECK (length(trading_day) = 10),
    status TEXT NOT NULL CHECK (status IN ('recorded', 'unavailable')),
    minute_count INTEGER NOT NULL CHECK (minute_count >= 0),
    checked_at TEXT NOT NULL,
    PRIMARY KEY (symbol, trading_day),
    CHECK ((status = 'unavailable') = (minute_count = 0))
) WITHOUT ROWID;
