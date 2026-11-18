DROP INDEX research_horizon_labels_jquants_idx;
ALTER TABLE research_horizon_labels DROP COLUMN exit_source;
DROP TABLE research_jquants_fetch_log;
DROP TABLE research_jquants_coverage;
DROP TABLE research_jquants_minute_bars;
DROP TABLE research_jquants_daily_bars;
DROP TABLE research_jquants_symbols;
