-- 全 TEXT 日時列: 保存済みの可変幅 RFC3339Nano（小数秒の末尾ゼロ切り詰め／小数部なし）を
-- 固定 9 桁の小数秒（`2006-01-02T15:04:05.000000000Z`）へ正規化する（issue #430）。
-- 従来の sqlutil.FormatTime は time.RFC3339Nano で書いたため、同一秒内で
-- `05Z` > `05.5Z`（'Z' 0x5A > '.' 0x2E）と辞書順が時刻順に一致せず、ClaimNext の
-- `scheduled_at <= ?` / ORDER BY や各 `timestamp >= ? AND timestamp < ?` の窓境界が
-- 同一秒内でずれていた。以降の書き込みは FormatTime が固定幅で行うため、既存行も
-- 同じ幅へ揃えて辞書順＝時刻順を保証する。
-- 対象は `YYYY-MM-DDTHH:MM:SS[.f…]Z`（UTC）形式で長さが 30 でない値のみ。NULL・既に
-- 固定幅の値・UTC 以外の表記は変更しない。ParseTime は桁数を問わず受理するため
-- 読み出し側は新旧どちらも読める。
-- kill_switch_events / kill_switch_resolutions は追記専用トリガー（000017）が UPDATE を
-- 拒否するため、正規化の間だけトリガーを外し、000017 と同一定義で作り直す。

DROP TRIGGER kill_switch_events_no_update;
DROP TRIGGER kill_switch_events_no_delete;
DROP TRIGGER kill_switch_resolutions_no_update;
DROP TRIGGER kill_switch_resolutions_no_delete;

UPDATE instruments SET
    created_at = CASE WHEN (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30) THEN
            substr(created_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(created_at, 20, 1) = '.' THEN substr(created_at, 21, length(created_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE created_at END,
    updated_at = CASE WHEN (updated_at LIKE '____-__-__T__:__:__%Z' AND substr(updated_at, 20, 1) IN ('.', 'Z') AND length(updated_at) <> 30) THEN
            substr(updated_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(updated_at, 20, 1) = '.' THEN substr(updated_at, 21, length(updated_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE updated_at END
WHERE (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30)
    OR (updated_at LIKE '____-__-__T__:__:__%Z' AND substr(updated_at, 20, 1) IN ('.', 'Z') AND length(updated_at) <> 30);

UPDATE market_snapshots SET
    timestamp = CASE WHEN (timestamp LIKE '____-__-__T__:__:__%Z' AND substr(timestamp, 20, 1) IN ('.', 'Z') AND length(timestamp) <> 30) THEN
            substr(timestamp, 1, 19) || '.' ||
            substr(CASE WHEN substr(timestamp, 20, 1) = '.' THEN substr(timestamp, 21, length(timestamp) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE timestamp END,
    created_at = CASE WHEN (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30) THEN
            substr(created_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(created_at, 20, 1) = '.' THEN substr(created_at, 21, length(created_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE created_at END
WHERE (timestamp LIKE '____-__-__T__:__:__%Z' AND substr(timestamp, 20, 1) IN ('.', 'Z') AND length(timestamp) <> 30)
    OR (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30);

UPDATE jobs SET
    scheduled_at = CASE WHEN (scheduled_at LIKE '____-__-__T__:__:__%Z' AND substr(scheduled_at, 20, 1) IN ('.', 'Z') AND length(scheduled_at) <> 30) THEN
            substr(scheduled_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(scheduled_at, 20, 1) = '.' THEN substr(scheduled_at, 21, length(scheduled_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE scheduled_at END,
    started_at = CASE WHEN (started_at LIKE '____-__-__T__:__:__%Z' AND substr(started_at, 20, 1) IN ('.', 'Z') AND length(started_at) <> 30) THEN
            substr(started_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(started_at, 20, 1) = '.' THEN substr(started_at, 21, length(started_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE started_at END,
    finished_at = CASE WHEN (finished_at LIKE '____-__-__T__:__:__%Z' AND substr(finished_at, 20, 1) IN ('.', 'Z') AND length(finished_at) <> 30) THEN
            substr(finished_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(finished_at, 20, 1) = '.' THEN substr(finished_at, 21, length(finished_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE finished_at END,
    created_at = CASE WHEN (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30) THEN
            substr(created_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(created_at, 20, 1) = '.' THEN substr(created_at, 21, length(created_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE created_at END
WHERE (scheduled_at LIKE '____-__-__T__:__:__%Z' AND substr(scheduled_at, 20, 1) IN ('.', 'Z') AND length(scheduled_at) <> 30)
    OR (started_at LIKE '____-__-__T__:__:__%Z' AND substr(started_at, 20, 1) IN ('.', 'Z') AND length(started_at) <> 30)
    OR (finished_at LIKE '____-__-__T__:__:__%Z' AND substr(finished_at, 20, 1) IN ('.', 'Z') AND length(finished_at) <> 30)
    OR (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30);

UPDATE jev_decisions SET
    timestamp = CASE WHEN (timestamp LIKE '____-__-__T__:__:__%Z' AND substr(timestamp, 20, 1) IN ('.', 'Z') AND length(timestamp) <> 30) THEN
            substr(timestamp, 1, 19) || '.' ||
            substr(CASE WHEN substr(timestamp, 20, 1) = '.' THEN substr(timestamp, 21, length(timestamp) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE timestamp END,
    created_at = CASE WHEN (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30) THEN
            substr(created_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(created_at, 20, 1) = '.' THEN substr(created_at, 21, length(created_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE created_at END
WHERE (timestamp LIKE '____-__-__T__:__:__%Z' AND substr(timestamp, 20, 1) IN ('.', 'Z') AND length(timestamp) <> 30)
    OR (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30);

UPDATE calibration_outcomes SET
    created_at = CASE WHEN (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30) THEN
            substr(created_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(created_at, 20, 1) = '.' THEN substr(created_at, 21, length(created_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE created_at END
WHERE (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30);

UPDATE trade_signals SET
    timestamp = CASE WHEN (timestamp LIKE '____-__-__T__:__:__%Z' AND substr(timestamp, 20, 1) IN ('.', 'Z') AND length(timestamp) <> 30) THEN
            substr(timestamp, 1, 19) || '.' ||
            substr(CASE WHEN substr(timestamp, 20, 1) = '.' THEN substr(timestamp, 21, length(timestamp) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE timestamp END,
    created_at = CASE WHEN (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30) THEN
            substr(created_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(created_at, 20, 1) = '.' THEN substr(created_at, 21, length(created_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE created_at END
WHERE (timestamp LIKE '____-__-__T__:__:__%Z' AND substr(timestamp, 20, 1) IN ('.', 'Z') AND length(timestamp) <> 30)
    OR (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30);

UPDATE paper_orders SET
    submitted_at = CASE WHEN (submitted_at LIKE '____-__-__T__:__:__%Z' AND substr(submitted_at, 20, 1) IN ('.', 'Z') AND length(submitted_at) <> 30) THEN
            substr(submitted_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(submitted_at, 20, 1) = '.' THEN substr(submitted_at, 21, length(submitted_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE submitted_at END,
    filled_at = CASE WHEN (filled_at LIKE '____-__-__T__:__:__%Z' AND substr(filled_at, 20, 1) IN ('.', 'Z') AND length(filled_at) <> 30) THEN
            substr(filled_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(filled_at, 20, 1) = '.' THEN substr(filled_at, 21, length(filled_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE filled_at END,
    created_at = CASE WHEN (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30) THEN
            substr(created_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(created_at, 20, 1) = '.' THEN substr(created_at, 21, length(created_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE created_at END
WHERE (submitted_at LIKE '____-__-__T__:__:__%Z' AND substr(submitted_at, 20, 1) IN ('.', 'Z') AND length(submitted_at) <> 30)
    OR (filled_at LIKE '____-__-__T__:__:__%Z' AND substr(filled_at, 20, 1) IN ('.', 'Z') AND length(filled_at) <> 30)
    OR (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30);

UPDATE positions SET
    opened_at = CASE WHEN (opened_at LIKE '____-__-__T__:__:__%Z' AND substr(opened_at, 20, 1) IN ('.', 'Z') AND length(opened_at) <> 30) THEN
            substr(opened_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(opened_at, 20, 1) = '.' THEN substr(opened_at, 21, length(opened_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE opened_at END,
    closed_at = CASE WHEN (closed_at LIKE '____-__-__T__:__:__%Z' AND substr(closed_at, 20, 1) IN ('.', 'Z') AND length(closed_at) <> 30) THEN
            substr(closed_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(closed_at, 20, 1) = '.' THEN substr(closed_at, 21, length(closed_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE closed_at END,
    created_at = CASE WHEN (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30) THEN
            substr(created_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(created_at, 20, 1) = '.' THEN substr(created_at, 21, length(created_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE created_at END,
    updated_at = CASE WHEN (updated_at LIKE '____-__-__T__:__:__%Z' AND substr(updated_at, 20, 1) IN ('.', 'Z') AND length(updated_at) <> 30) THEN
            substr(updated_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(updated_at, 20, 1) = '.' THEN substr(updated_at, 21, length(updated_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE updated_at END
WHERE (opened_at LIKE '____-__-__T__:__:__%Z' AND substr(opened_at, 20, 1) IN ('.', 'Z') AND length(opened_at) <> 30)
    OR (closed_at LIKE '____-__-__T__:__:__%Z' AND substr(closed_at, 20, 1) IN ('.', 'Z') AND length(closed_at) <> 30)
    OR (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30)
    OR (updated_at LIKE '____-__-__T__:__:__%Z' AND substr(updated_at, 20, 1) IN ('.', 'Z') AND length(updated_at) <> 30);

UPDATE policy_proposals SET
    proposed_at = CASE WHEN (proposed_at LIKE '____-__-__T__:__:__%Z' AND substr(proposed_at, 20, 1) IN ('.', 'Z') AND length(proposed_at) <> 30) THEN
            substr(proposed_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(proposed_at, 20, 1) = '.' THEN substr(proposed_at, 21, length(proposed_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE proposed_at END,
    applied_at = CASE WHEN (applied_at LIKE '____-__-__T__:__:__%Z' AND substr(applied_at, 20, 1) IN ('.', 'Z') AND length(applied_at) <> 30) THEN
            substr(applied_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(applied_at, 20, 1) = '.' THEN substr(applied_at, 21, length(applied_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE applied_at END,
    rolled_back_at = CASE WHEN (rolled_back_at LIKE '____-__-__T__:__:__%Z' AND substr(rolled_back_at, 20, 1) IN ('.', 'Z') AND length(rolled_back_at) <> 30) THEN
            substr(rolled_back_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(rolled_back_at, 20, 1) = '.' THEN substr(rolled_back_at, 21, length(rolled_back_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE rolled_back_at END,
    created_at = CASE WHEN (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30) THEN
            substr(created_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(created_at, 20, 1) = '.' THEN substr(created_at, 21, length(created_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE created_at END
WHERE (proposed_at LIKE '____-__-__T__:__:__%Z' AND substr(proposed_at, 20, 1) IN ('.', 'Z') AND length(proposed_at) <> 30)
    OR (applied_at LIKE '____-__-__T__:__:__%Z' AND substr(applied_at, 20, 1) IN ('.', 'Z') AND length(applied_at) <> 30)
    OR (rolled_back_at LIKE '____-__-__T__:__:__%Z' AND substr(rolled_back_at, 20, 1) IN ('.', 'Z') AND length(rolled_back_at) <> 30)
    OR (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30);

UPDATE runtime_settings SET
    updated_at = CASE WHEN (updated_at LIKE '____-__-__T__:__:__%Z' AND substr(updated_at, 20, 1) IN ('.', 'Z') AND length(updated_at) <> 30) THEN
            substr(updated_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(updated_at, 20, 1) = '.' THEN substr(updated_at, 21, length(updated_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE updated_at END
WHERE (updated_at LIKE '____-__-__T__:__:__%Z' AND substr(updated_at, 20, 1) IN ('.', 'Z') AND length(updated_at) <> 30);

UPDATE secrets SET
    updated_at = CASE WHEN (updated_at LIKE '____-__-__T__:__:__%Z' AND substr(updated_at, 20, 1) IN ('.', 'Z') AND length(updated_at) <> 30) THEN
            substr(updated_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(updated_at, 20, 1) = '.' THEN substr(updated_at, 21, length(updated_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE updated_at END
WHERE (updated_at LIKE '____-__-__T__:__:__%Z' AND substr(updated_at, 20, 1) IN ('.', 'Z') AND length(updated_at) <> 30);

UPDATE kill_switch_events SET
    triggered_at = CASE WHEN (triggered_at LIKE '____-__-__T__:__:__%Z' AND substr(triggered_at, 20, 1) IN ('.', 'Z') AND length(triggered_at) <> 30) THEN
            substr(triggered_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(triggered_at, 20, 1) = '.' THEN substr(triggered_at, 21, length(triggered_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE triggered_at END,
    created_at = CASE WHEN (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30) THEN
            substr(created_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(created_at, 20, 1) = '.' THEN substr(created_at, 21, length(created_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE created_at END
WHERE (triggered_at LIKE '____-__-__T__:__:__%Z' AND substr(triggered_at, 20, 1) IN ('.', 'Z') AND length(triggered_at) <> 30)
    OR (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30);

UPDATE kill_switch_resolutions SET
    resolved_at = CASE WHEN (resolved_at LIKE '____-__-__T__:__:__%Z' AND substr(resolved_at, 20, 1) IN ('.', 'Z') AND length(resolved_at) <> 30) THEN
            substr(resolved_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(resolved_at, 20, 1) = '.' THEN substr(resolved_at, 21, length(resolved_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE resolved_at END,
    created_at = CASE WHEN (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30) THEN
            substr(created_at, 1, 19) || '.' ||
            substr(CASE WHEN substr(created_at, 20, 1) = '.' THEN substr(created_at, 21, length(created_at) - 21) ELSE '' END || '000000000', 1, 9) || 'Z'
        ELSE created_at END
WHERE (resolved_at LIKE '____-__-__T__:__:__%Z' AND substr(resolved_at, 20, 1) IN ('.', 'Z') AND length(resolved_at) <> 30)
    OR (created_at LIKE '____-__-__T__:__:__%Z' AND substr(created_at, 20, 1) IN ('.', 'Z') AND length(created_at) <> 30);

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
