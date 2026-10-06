-- jev_decisions: (decision_type, direction, confidence) 索引。Jev Trader ジョブごとの
-- Calibration 判定（CalibrationRepository.CountLabeledSamplesInConfidenceRange、
-- issue #603）は decision_type='trader' AND direction IN ('LONG','SHORT') AND
-- confidence の範囲で件数を数える。既存索引（decision_type, timestamp）では
-- confidence 範囲を絞れず、trader 判断の全履歴を 1 行ずつ辿っていた。
-- この索引で confidence 範囲が索引範囲検索になり、LIMIT（閾値）と合わせて走査量が
-- 履歴の行数に依存しない。
CREATE INDEX jev_decisions_type_direction_confidence_idx ON jev_decisions (decision_type, direction, confidence);
