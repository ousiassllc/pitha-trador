-- 判断時点の相場状態（レジーム）と A/B アームを jev_decisions に保存する（issue #755、親 #751、NULL可）。
-- market_regime は市場指数の 1 分足リターンのボラティリティと自己相関から判定した trend / mean_revert / unknown、
-- ab_arm はモメンタム固定（momentum）とレジーム切替（regime）のどちらの戦略で判断したか。
-- regime_volatility_bps / regime_autocorr は判定の根拠、vwap_z は平均回帰レジームでの VWAP 乖離 z スコア。
-- いずれも未評価（旧行・判定器なし）は NULL。
ALTER TABLE jev_decisions ADD COLUMN market_regime VARCHAR(16)
    CHECK (market_regime IS NULL OR market_regime IN ('trend', 'mean_revert', 'unknown'));
ALTER TABLE jev_decisions ADD COLUMN ab_arm VARCHAR(16)
    CHECK (ab_arm IS NULL OR ab_arm IN ('momentum', 'regime'));
ALTER TABLE jev_decisions ADD COLUMN regime_volatility_bps REAL;
ALTER TABLE jev_decisions ADD COLUMN regime_autocorr REAL;
ALTER TABLE jev_decisions ADD COLUMN vwap_z REAL;
