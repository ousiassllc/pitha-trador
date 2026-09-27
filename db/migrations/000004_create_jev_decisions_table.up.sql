-- jev_decisions: Jev Scout/Traderの入出力ログ（Calibrationの基礎データ）
-- （docs/architecture/er.md §jev_decisions）
CREATE TABLE jev_decisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instrument_id INTEGER NOT NULL REFERENCES instruments (id),
    symbol VARCHAR(10) NOT NULL,
    timestamp TEXT NOT NULL,
    decision_type VARCHAR(10) NOT NULL
        CHECK (decision_type IN ('scout', 'trader')),
    state_hash VARCHAR(64) NOT NULL,
    state_json TEXT NOT NULL,
    question_version VARCHAR(20) NOT NULL,
    response_json TEXT NOT NULL,
    direction VARCHAR(10)
        CHECK (direction IN ('LONG', 'SHORT', 'NONE')),
    confidence NUMERIC(5, 4),
    latency_ms INTEGER NOT NULL,
    model_id VARCHAR(100) NOT NULL,
    request_cost NUMERIC(10, 6),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX jev_decisions_instrument_timestamp_idx
    ON jev_decisions (instrument_id, timestamp DESC);
CREATE INDEX jev_decisions_decision_type_idx ON jev_decisions (decision_type);
CREATE INDEX jev_decisions_state_hash_idx ON jev_decisions (state_hash);
