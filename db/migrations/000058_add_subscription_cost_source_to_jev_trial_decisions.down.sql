-- 000058 の巻き戻し: 'subscription' の行（旧スキーマに表現できない）を消し、CHECK の文言を 000054 のものに戻す。
DELETE FROM jev_trial_decisions WHERE cost_source = 'subscription';
PRAGMA writable_schema = ON;
UPDATE sqlite_master SET sql = replace(replace(sql,
    '''list_price_estimate'', ''unknown'', ''subscription'')', '''list_price_estimate'', ''unknown'')'),
    'CHECK (counted_cost_usd >= 0 AND (cost_source <> ''subscription'' OR (counted_cost_usd = 0 AND cost_usd IS NULL)))', 'CHECK (counted_cost_usd >= 0)')
WHERE type = 'table' AND name = 'jev_trial_decisions';
PRAGMA writable_schema = RESET;
DROP INDEX jev_trial_decisions_provider_idx;
