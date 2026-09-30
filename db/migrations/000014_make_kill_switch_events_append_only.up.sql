-- kill_switch_events を追記専用の監査ログにする
-- （docs/requirements/non-functional.md §4, docs/architecture/er.md
-- §kill_switch_events / §kill_switch_resolutions, issue #102）。
--
-- これまで Kill Switch の解除は kill_switch_events 行の resolved_at /
-- resolved_by を UPDATE していたため、監査ログが書き換え可能だった。
-- 解除は別テーブル kill_switch_resolutions への INSERT（1イベントにつき
-- 高々1行）として記録し、両テーブルの UPDATE/DELETE をトリガーで拒否する。
CREATE TABLE kill_switch_resolutions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kill_switch_event_id INTEGER NOT NULL UNIQUE
        REFERENCES kill_switch_events (id),
    resolved_at TEXT NOT NULL,
    resolved_by VARCHAR(50) NOT NULL
        CHECK (resolved_by IN ('auto', 'manual')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- 既存の解除済み行を移行してから、更新可能だった列を廃止する。
INSERT INTO kill_switch_resolutions (kill_switch_event_id, resolved_at, resolved_by)
SELECT id, resolved_at, resolved_by
FROM kill_switch_events
WHERE resolved_at IS NOT NULL AND resolved_by IS NOT NULL;

DROP INDEX kill_switch_events_resolved_at_idx;
ALTER TABLE kill_switch_events DROP COLUMN resolved_at;
ALTER TABLE kill_switch_events DROP COLUMN resolved_by;

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
