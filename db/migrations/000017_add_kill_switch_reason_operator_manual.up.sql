-- kill_switch_events.reason に手動 Kill 用の 'operator_manual' を追加する
-- （POST /api/v1/system/kill の監査ログ記録、FR-RISK-5・UC-11、issue #169）。
--
-- SQLite は CHECK 制約を ALTER できないため、kill_switch_events と、それを
-- 参照する kill_switch_resolutions を新テーブルへ作り直して行を移行する。
-- 追記専用トリガー（000014）は DROP TABLE で消えるので再作成する。
CREATE TABLE kill_switch_events_new (
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
            'operator_heartbeat_timeout',
            'operator_manual'
        )),
    detail_json TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE kill_switch_resolutions_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kill_switch_event_id INTEGER NOT NULL UNIQUE
        REFERENCES kill_switch_events_new (id),
    resolved_at TEXT NOT NULL,
    resolved_by VARCHAR(50) NOT NULL
        CHECK (resolved_by IN ('auto', 'manual')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO kill_switch_events_new (id, triggered_at, reason, detail_json, created_at)
SELECT id, triggered_at, reason, detail_json, created_at FROM kill_switch_events;
INSERT INTO kill_switch_resolutions_new (id, kill_switch_event_id, resolved_at, resolved_by, created_at)
SELECT id, kill_switch_event_id, resolved_at, resolved_by, created_at FROM kill_switch_resolutions;

DROP TABLE kill_switch_resolutions;
DROP TABLE kill_switch_events;
ALTER TABLE kill_switch_events_new RENAME TO kill_switch_events;
ALTER TABLE kill_switch_resolutions_new RENAME TO kill_switch_resolutions;

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
