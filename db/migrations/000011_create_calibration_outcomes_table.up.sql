-- calibration_outcomes: Jev判断（jev_decisions, decision_type=trader）と
-- 将来値動きの紐付け結果（docs/architecture/er.md §calibration_outcomes）
CREATE TABLE calibration_outcomes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    jev_decision_id INTEGER NOT NULL REFERENCES jev_decisions (id),
    horizon_minutes INTEGER NOT NULL,
    future_return NUMERIC(8, 4) NOT NULL,
    max_adverse_excursion NUMERIC(8, 4) NOT NULL,
    max_favorable_excursion NUMERIC(8, 4) NOT NULL,
    was_direction_correct BOOLEAN,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE UNIQUE INDEX calibration_outcomes_decision_horizon_idx
    ON calibration_outcomes (jev_decision_id, horizon_minutes);
