-- 資産ごとの予算枠の下準備（issue #824、親 #823）。
-- instruments.asset_class: 銘柄の資産クラス。'stock'（株）／'bond_etf'（債券ETF）。kind（株式か指数か）とは別の軸で、
--   kind の意味と CHECK は変えない。既存行・指定なしの INSERT は 'stock'。
-- positions.bucket / paper_orders.bucket / trade_signals.bucket: その行を書いた時点の銘柄の asset_class から確定させた枠。
--   書き込み時に確定し、あとから asset_class や枠の設定を変えても過去の行は変わらない。
--   既存行は DEFAULT 'stock' で埋まる（この migration 以前は株だけだった）。
ALTER TABLE instruments ADD COLUMN asset_class VARCHAR(20) NOT NULL DEFAULT 'stock'
    CHECK (asset_class IN ('stock', 'bond_etf'));
ALTER TABLE positions ADD COLUMN bucket VARCHAR(20) NOT NULL DEFAULT 'stock'
    CHECK (bucket IN ('stock', 'bond_etf'));
ALTER TABLE paper_orders ADD COLUMN bucket VARCHAR(20) NOT NULL DEFAULT 'stock'
    CHECK (bucket IN ('stock', 'bond_etf'));
ALTER TABLE trade_signals ADD COLUMN bucket VARCHAR(20) NOT NULL DEFAULT 'stock'
    CHECK (bucket IN ('stock', 'bond_etf'));
