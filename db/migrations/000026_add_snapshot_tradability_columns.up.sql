-- market_snapshots: 約定不能・不利な候補をエントリー前に外すための取引可否カラム（issue #511）
--   special_quote: 板の気配が特別気配（BidSign/AskSign = 0102 特別気配 / 0108 停止前特別気配）か。0/1、既定0
--   price_limit:   ストップ高/安（現値が値幅上限/下限に張り付いている）か。''=該当なし / 'up' / 'down'
--   lendable:      貸借銘柄か（kabuステーションAPI /symbol の MarginSell＝制度信用売建可）。
--                  NULL=不明（本マイグレーション以前の行、または銘柄情報の取得失敗）。FALSE のときだけショートを外す
-- （docs/architecture/er/tables-market.md §market_snapshots、functional.md FR-FS-1/FR-POLICY-3）
ALTER TABLE market_snapshots ADD COLUMN special_quote INTEGER NOT NULL DEFAULT 0 CHECK (special_quote IN (0, 1));
ALTER TABLE market_snapshots ADD COLUMN price_limit VARCHAR(10) NOT NULL DEFAULT '' CHECK (price_limit IN ('', 'up', 'down'));
ALTER TABLE market_snapshots ADD COLUMN lendable INTEGER CHECK (lendable IN (0, 1));
