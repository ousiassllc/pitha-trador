-- 000017 の巻き戻し: 'operator_manual' を許さない CHECK に戻す。
-- 'operator_manual' の行は旧スキーマに表現できないため、その解除行とともに
-- 移行対象から外す（コピー時に除外するだけで DELETE は発行しないので、
-- 追記専用トリガーには触れない）。
CREATE TABLE kill_switch_events_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    triggered_at TEXT NOT NULL,
    reason VARCHAR(100) NOT NULL
        CHECK (reason IN (
            'daily_loss_limit',
            'consecutive_losses',
            'market_data_down',
            'jev_api_down',
            'broker_api_error',
            'unexpected_position',
            'fill_discrepancy',
            'db_write_failure',
            'operator_heartbeat_timeout'
        )),
    detail_json TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE kill_switch_resolutions_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kill_switch_event_id INTEGER NOT NULL UNIQUE
        REFERENCES kill_switch_events_old (id),
    resolved_at TEXT NOT NULL,
    resolved_by VARCHAR(50) NOT NULL
        CHECK (resolved_by IN ('auto', 'manual')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO kill_switch_events_old (id, triggered_at, reason, detail_json, created_at)
SELECT id, triggered_at, reason, detail_json, created_at
FROM kill_switch_events WHERE reason != 'operator_manual';
INSERT INTO kill_switch_resolutions_old (id, kill_switch_event_id, resolved_at, resolved_by, created_at)
SELECT id, kill_switch_event_id, resolved_at, resolved_by, created_at
FROM kill_switch_resolutions
WHERE kill_switch_event_id IN (SELECT id FROM kill_switch_events_old);

DROP TABLE kill_switch_resolutions;
DROP TABLE kill_switch_events;
ALTER TABLE kill_switch_events_old RENAME TO kill_switch_events;
ALTER TABLE kill_switch_resolutions_old RENAME TO kill_switch_resolutions;

CREATE INDEX kill_switch_events_triggered_at_idx
    ON kill_switch_events (triggered_at DESC);

CREATE TRIGGER kill_switch_events_no_update
BEFORE UPDATE ON kill_switch_events
BEGIN
    SELECT RAISE(ABORT, 'kill_switch_events is append-only: UPDATE is not allowed');
END;

CREATE TRIGGER kill_switch_events_no_delete
BEFORE DELETE ON kill_switch_events
BEGIN
    SELECT RAISE(ABORT, 'kill_switch_events is append-only: DELETE is not allowed');
END;

CREATE TRIGGER kill_switch_resolutions_no_update
BEFORE UPDATE ON kill_switch_resolutions
BEGIN
    SELECT RAISE(ABORT, 'kill_switch_resolutions is append-only: UPDATE is not allowed');
END;

CREATE TRIGGER kill_switch_resolutions_no_delete
BEFORE DELETE ON kill_switch_resolutions
BEGIN
    SELECT RAISE(ABORT, 'kill_switch_resolutions is append-only: DELETE is not allowed');
END;
