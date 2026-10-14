-- 板の需給偏り（OFI）とマイクロプライス乖離の特徴量列を追加（issue #750、すべてNULL可、FR-FE-2）。
-- market_snapshots は算出値、jev_decisions は判断時点の値のコピー（後から学習・分析に使う）。
ALTER TABLE market_snapshots ADD COLUMN ofi_short NUMERIC;
ALTER TABLE market_snapshots ADD COLUMN ofi_mid NUMERIC;
ALTER TABLE market_snapshots ADD COLUMN ofi_long NUMERIC;
ALTER TABLE market_snapshots ADD COLUMN ofi_window_short_seconds INTEGER;
ALTER TABLE market_snapshots ADD COLUMN ofi_window_mid_seconds INTEGER;
ALTER TABLE market_snapshots ADD COLUMN ofi_window_long_seconds INTEGER;
ALTER TABLE market_snapshots ADD COLUMN microprice_dev_bps NUMERIC;
ALTER TABLE jev_decisions ADD COLUMN ofi_short NUMERIC;
ALTER TABLE jev_decisions ADD COLUMN ofi_mid NUMERIC;
ALTER TABLE jev_decisions ADD COLUMN ofi_long NUMERIC;
ALTER TABLE jev_decisions ADD COLUMN ofi_window_short_seconds INTEGER;
ALTER TABLE jev_decisions ADD COLUMN ofi_window_mid_seconds INTEGER;
ALTER TABLE jev_decisions ADD COLUMN ofi_window_long_seconds INTEGER;
ALTER TABLE jev_decisions ADD COLUMN microprice_dev_bps NUMERIC;
