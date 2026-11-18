-- cost_source に 'subscription'（ChatGPT のサブスク。USD なし。cost_usd は NULL・counted_cost_usd は 0）を許す（issue #1081）。
-- SQLite は CHECK を ALTER できないので、SQLite 公式の表の作り直し（lang_altertable.html「Making Other Kinds Of Table Schema Changes」）で
-- 新しい表を作る → 行をコピー → 古い表を消す → 名前を変える。jev_trial_decisions を参照する表・トリガー・ビューは無い
-- （参照するのは jev_decisions への外部キーだけで、新しい表にも同じ形で付ける）。古い表と一緒に消えるインデックスは作り直す
-- （000054 の 2 本と、プロバイダー別の回数・トークン数の集計用の provider_idx）。
CREATE TABLE jev_trial_decisions_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    jev_decision_id INTEGER NOT NULL REFERENCES jev_decisions (id),
    arm_id VARCHAR(48) NOT NULL CHECK (arm_id <> ''),
    prompt_version VARCHAR(40) NOT NULL CHECK (prompt_version <> ''),
    provider VARCHAR(40) NOT NULL DEFAULT '',
    requested_model VARCHAR(100) NOT NULL CHECK (requested_model <> ''),
    served_model VARCHAR(100) NOT NULL DEFAULT '',
    served_provider VARCHAR(100) NOT NULL DEFAULT '',
    response_id VARCHAR(100) NOT NULL DEFAULT '',
    status VARCHAR(8) NOT NULL CHECK (status IN ('ok', 'failed', 'refused', 'partial')),
    failure_code VARCHAR(24) NOT NULL DEFAULT '',
    http_status INTEGER,
    error_excerpt VARCHAR(200) NOT NULL DEFAULT '',
    direction VARCHAR(10) CHECK (direction IS NULL OR direction IN ('LONG', 'SHORT', 'NONE')),
    regime VARCHAR(10) CHECK (regime IS NULL OR regime IN ('TREND', 'RANGE', 'BREAKOUT', 'CHAOTIC')),
    entry_quality VARCHAR(12) CHECK (entry_quality IS NULL OR entry_quality IN ('poor', 'fair', 'good', 'strong', 'exceptional')),
    confidence NUMERIC(5, 4) CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1)),
    toxic_flow NUMERIC(5, 4) CHECK (toxic_flow IS NULL OR (toxic_flow >= 0 AND toxic_flow <= 1)),
    liquidity_stressed NUMERIC(5, 4) CHECK (liquidity_stressed IS NULL OR (liquidity_stressed >= 0 AND liquidity_stressed <= 1)),
    continuation_probability NUMERIC(5, 4)
        CHECK (continuation_probability IS NULL OR (continuation_probability >= 0 AND continuation_probability <= 1)),
    response_json TEXT,
    latency_ms INTEGER NOT NULL DEFAULT 0,
    input_tokens INTEGER CHECK (input_tokens IS NULL OR input_tokens >= 0),
    output_tokens INTEGER CHECK (output_tokens IS NULL OR output_tokens >= 0),
    cost_usd NUMERIC(12, 8) CHECK (cost_usd IS NULL OR cost_usd >= 0),
    cost_source VARCHAR(20) NOT NULL CHECK (cost_source IN ('reported', 'list_price_estimate', 'unknown', 'subscription')),
    counted_cost_usd NUMERIC(12, 8) NOT NULL DEFAULT 0
        CHECK (counted_cost_usd >= 0 AND (cost_source <> 'subscription' OR (counted_cost_usd = 0 AND cost_usd IS NULL))),
    request_bytes INTEGER NOT NULL DEFAULT 0 CHECK (request_bytes >= 0),
    state_bytes INTEGER NOT NULL DEFAULT 0 CHECK (state_bytes >= 0),
    truncation_suspected INTEGER NOT NULL DEFAULT 0 CHECK (truncation_suspected IN (0, 1)),
    llm_cutoff_status VARCHAR(20) NOT NULL DEFAULT 'unverified' CHECK (llm_cutoff_status <> ''),
    rag_includes_live_model_answers INTEGER NOT NULL DEFAULT 0 CHECK (rag_includes_live_model_answers IN (0, 1)),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (jev_decision_id, arm_id),
    CHECK (status <> 'ok' OR (direction IS NOT NULL AND regime IS NOT NULL AND entry_quality IS NOT NULL
        AND toxic_flow IS NOT NULL AND liquidity_stressed IS NOT NULL AND continuation_probability IS NOT NULL)),
    CHECK ((status = 'failed') = (failure_code <> ''))
);
INSERT INTO jev_trial_decisions_new SELECT * FROM jev_trial_decisions;
-- AUTOINCREMENT の採番位置（sqlite_sequence）も引き継ぐ（行を消した後でも id を再利用しない）。
DELETE FROM sqlite_sequence WHERE name = 'jev_trial_decisions_new';
INSERT INTO sqlite_sequence (name, seq) SELECT 'jev_trial_decisions_new', seq FROM sqlite_sequence WHERE name = 'jev_trial_decisions';
DROP TABLE jev_trial_decisions;
ALTER TABLE jev_trial_decisions_new RENAME TO jev_trial_decisions;
CREATE INDEX jev_trial_decisions_created_at_idx ON jev_trial_decisions (created_at);
CREATE INDEX jev_trial_decisions_arm_idx ON jev_trial_decisions (arm_id, created_at);
CREATE INDEX jev_trial_decisions_provider_idx ON jev_trial_decisions (provider, created_at);
