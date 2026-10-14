-- calibration_outcomes にトリプルバリア方式のラベル列を追加（issue #756、親 #751）。
-- barrier_hit: 先に触れたバリア（tp/sl/timeout）。旧ラベル（終値のみで判定）の行はNULL。
-- net_return_bps: 売買コスト控除後のリターン（bps）。旧ラベルの行はNULL。
-- entry_ref_price: 執行できる基準価格（LONGはask、SHORTはbid）。旧ラベルの行はNULL。
-- label_version: ラベル方式の版。1=旧ラベル（終値のみ。既存行は全てこの値で埋める）、2=トリプルバリア。
ALTER TABLE calibration_outcomes ADD COLUMN barrier_hit TEXT CHECK (barrier_hit IN ('tp', 'sl', 'timeout'));
ALTER TABLE calibration_outcomes ADD COLUMN net_return_bps NUMERIC;
ALTER TABLE calibration_outcomes ADD COLUMN entry_ref_price NUMERIC;
ALTER TABLE calibration_outcomes ADD COLUMN label_version INTEGER NOT NULL DEFAULT 1;
