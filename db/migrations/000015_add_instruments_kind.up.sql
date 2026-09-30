-- instruments.kind: 株式と、Feature Engineの市場コンテキスト入力に使う指数銘柄の区別
-- （docs/architecture/er/tables-market.md §instruments、functional.md §4.1）
ALTER TABLE instruments ADD COLUMN kind VARCHAR(20) NOT NULL DEFAULT 'stock'
    CHECK (kind IN ('stock', 'market_index', 'sector_index'));
