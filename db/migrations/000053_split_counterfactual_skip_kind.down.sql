DROP VIEW calibration_outcome_origins;
CREATE VIEW calibration_outcome_origins AS
SELECT o.id AS calibration_outcome_id,
    o.jev_decision_id AS jev_decision_id,
    o.horizon_minutes AS horizon_minutes,
    CASE
        WHEN d.direction IS NULL OR d.direction = 'NONE' THEN 'no_direction'
        WHEN EXISTS (SELECT 1 FROM trade_signals s WHERE s.jev_decision_id = d.id AND s.risk_passed = 1) THEN 'entered'
        ELSE 'skipped'
    END AS origin,
    (SELECT s2.reject_reason FROM trade_signals s2 WHERE s2.jev_decision_id = d.id ORDER BY s2.id DESC LIMIT 1) AS reject_reason
FROM calibration_outcomes o
JOIN jev_decisions d ON d.id = o.jev_decision_id;
ALTER TABLE counterfactual_labels DROP COLUMN cutoff_unverified;
ALTER TABLE counterfactual_labels DROP COLUMN skip_kind;
