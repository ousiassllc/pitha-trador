DROP TRIGGER IF EXISTS kill_switch_resolutions_no_delete;
DROP TRIGGER IF EXISTS kill_switch_resolutions_no_update;
DROP TRIGGER IF EXISTS kill_switch_events_no_delete;
DROP TRIGGER IF EXISTS kill_switch_events_no_update;

ALTER TABLE kill_switch_events ADD COLUMN resolved_at TEXT;
ALTER TABLE kill_switch_events ADD COLUMN resolved_by VARCHAR(50)
    CHECK (resolved_by IN ('auto', 'manual'));

UPDATE kill_switch_events
SET resolved_at = (SELECT r.resolved_at FROM kill_switch_resolutions r WHERE r.kill_switch_event_id = kill_switch_events.id),
    resolved_by = (SELECT r.resolved_by FROM kill_switch_resolutions r WHERE r.kill_switch_event_id = kill_switch_events.id);

CREATE INDEX kill_switch_events_resolved_at_idx
    ON kill_switch_events (resolved_at);

DROP TABLE IF EXISTS kill_switch_resolutions;
