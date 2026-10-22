ALTER TABLE metalabel_retrain_skips DROP COLUMN required_oos_rows;
ALTER TABLE metalabel_retrain_skips DROP COLUMN oos_rows;
ALTER TABLE metalabel_retrain_skips DROP COLUMN label_v2_rows;
ALTER TABLE metalabel_retrain_skips DROP COLUMN label_v1_rows;
ALTER TABLE metalabel_retrain_skips DROP COLUMN dropped_incomplete;
