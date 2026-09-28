-- trade_signals: Policy Engineが生成した取引候補（docs/architecture/er.md
-- §trade_signals）
CREATE TABLE trade_signals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instrument_id INTEGER NOT NULL REFERENCES instruments (id),
    jev_decision_id INTEGER REFERENCES jev_decisions (id),
    symbol VARCHAR(10) NOT NULL,
    timestamp TEXT NOT NULL,
    direction VARCHAR(10) NOT NULL
        CHECK (direction IN ('LONG', 'SHORT', 'NONE')),
    score NUMERIC(6, 4),
    entry_price_reference NUMERIC(12, 2),
    policy_version VARCHAR(20) NOT NULL,
    risk_passed BOOLEAN NOT NULL,
    reject_reason VARCHAR(255),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX trade_signals_instrument_timestamp_idx
    ON trade_signals (instrument_id, timestamp DESC);
CREATE INDEX trade_signals_risk_passed_idx ON trade_signals (risk_passed);
