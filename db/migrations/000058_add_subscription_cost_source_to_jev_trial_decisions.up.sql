-- cost_source に 'subscription'（ChatGPT のサブスク。USD なし。cost_usd は NULL・counted_cost_usd は 0）を許す（issue #1081）。
-- 表の作り直しはせず、SQLite 公式の writable_schema の手順（lang_altertable.html「otheralter」。CHECK の変更向け）で CHECK の文言だけを
-- 置き換える（行は動かさない）。RESET は、この接続を含めスキーマを読み直させる（これが無いと、移行を実行した接続だけ古い CHECK のまま残る）。
-- 末尾の CREATE INDEX は schema_version を進めて、他の接続にも読み直させる役を兼ねる（プロバイダー別の回数・トークン数の集計用）。
PRAGMA writable_schema = ON;
UPDATE sqlite_master SET sql = replace(replace(sql,
    '''list_price_estimate'', ''unknown'')', '''list_price_estimate'', ''unknown'', ''subscription'')'),
    'CHECK (counted_cost_usd >= 0)', 'CHECK (counted_cost_usd >= 0 AND (cost_source <> ''subscription'' OR (counted_cost_usd = 0 AND cost_usd IS NULL)))')
WHERE type = 'table' AND name = 'jev_trial_decisions';
PRAGMA writable_schema = RESET;
CREATE INDEX jev_trial_decisions_provider_idx ON jev_trial_decisions (provider, created_at);
