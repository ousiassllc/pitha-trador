-- jev_decisions: (decision_type, timestamp) / (timestamp) 索引。Activity Log の
-- DecisionRepository.ListRecent（issue #419）は
-- ORDER BY timestamp DESC, id DESC LIMIT N で直近 N 件だけを返す。いずれの
-- 既存索引（instrument_id 先頭・decision_type 単独・state_hash）でも並び順を
-- 満たせず、全 jev_decisions を走査して一時 B-tree で整列していた。
-- 昇順の索引を逆順に走査すると (timestamp DESC, 暗黙の rowid=id DESC) となり、
-- ORDER BY と一致して LIMIT 件で打ち切れる（DESC 索引では id が昇順になり一致しない）。
-- decision_type 指定あり（Scout/Trader 別）は前者、指定なしは後者で引く。
CREATE INDEX jev_decisions_type_timestamp_idx ON jev_decisions (decision_type, timestamp);
CREATE INDEX jev_decisions_timestamp_idx ON jev_decisions (timestamp);
