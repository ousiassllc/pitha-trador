-- calibration_label_skips: 水平線まで足が揃わないことが確定した
-- (jev_decision_id, horizon_minutes) の終端マーカー（FR-CAL-4、issue #481）。
-- calibration_outcomes は作らない（短縮horizonを記録しない）まま、
-- PendingLabels が当該ペアを再投入対象から外すために使う。
CREATE TABLE calibration_label_skips (
    jev_decision_id INTEGER NOT NULL REFERENCES jev_decisions (id),
    horizon_minutes INTEGER NOT NULL,
    reason TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (jev_decision_id, horizon_minutes)
);
