-- jev_decisions: (instrument_id, decision_type, timestamp DESC, id DESC) 索引。
-- 「銘柄ごとの最新 Trader/Scout 判断」（DecisionRepository.LatestTraderByInstruments /
-- LatestTrader / LatestScout、issue #496/#497/#498/#499）を、銘柄 × decision_type の索引
-- seek で先頭 1 行だけ読んで引くため。既存の (instrument_id, timestamp DESC) では
-- decision_type を条件に入れられず、(decision_type, timestamp) では instrument_id を
-- 条件に入れられないため、全 Trader 行の走査になっていた。
-- ORDER BY timestamp DESC, id DESC LIMIT 1 と索引の並びが一致し、整列は発生しない。
CREATE INDEX jev_decisions_instrument_type_timestamp_idx
    ON jev_decisions (instrument_id, decision_type, timestamp DESC, id DESC);
