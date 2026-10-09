-- watch_lists / watch_list_entries: 立花 e支店APIの監視リスト（最大120銘柄）を、翌営業日分として
-- 引け後に確定して保存する（issue #730、親 #726、docs/architecture/er/tables-market.md §watch_lists）。
-- 翌朝の EVENT 購読・market-data 投入・Fast Screener の母集団になる。1立会日1リスト。
-- 日足から導出した銘柄コードと選定理由だけを持ち、価格の生値は持たない（外部へ出す経路も作らない、#720）。
-- source は確定の仕方: daily_screen=日足スクリーニング / fixed=運用者の固定リスト /
-- carried_over=日足が取れず前営業日のリストを引き継いだ / fixed_fallback=日足が取れず固定リストへ退避した。
CREATE TABLE watch_lists (
    list_date TEXT PRIMARY KEY CHECK (list_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    source TEXT NOT NULL CHECK (source IN ('daily_screen', 'fixed', 'carried_over', 'fixed_fallback')),
    reason TEXT NOT NULL DEFAULT '',
    basis_date TEXT NOT NULL DEFAULT '',
    decided_at TEXT NOT NULL
);

-- origin は枠の由来: held=保有・注文中の固定枠 / manual=手動指定 / screen=スクリーニング上位 / fixed=固定リスト。
-- indicators は screen のとき選んだ指標（カンマ区切り、例: gain_rate,volume_surge）。
-- position は表示・購読順（0 起点、held が先頭）。
CREATE TABLE watch_list_entries (
    list_date TEXT NOT NULL,
    position INTEGER NOT NULL CHECK (position >= 0),
    symbol VARCHAR(10) NOT NULL,
    origin TEXT NOT NULL CHECK (origin IN ('held', 'manual', 'screen', 'fixed')),
    indicators TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (list_date, position)
) WITHOUT ROWID;
