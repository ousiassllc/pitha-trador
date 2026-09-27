-- kill_switch_events: Risk EngineのKill Switch発動・解除履歴（監査ログ）
-- （docs/architecture/er.md §kill_switch_events）
CREATE TABLE kill_switch_events (
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
    resolved_at TEXT,
    resolved_by VARCHAR(50)
        CHECK (resolved_by IN ('auto', 'manual')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX kill_switch_events_triggered_at_idx
    ON kill_switch_events (triggered_at DESC);
CREATE INDEX kill_switch_events_resolved_at_idx
    ON kill_switch_events (resolved_at);
