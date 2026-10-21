-- jev_shadow_decisions / jev_prompt_promotions: Jev プロンプトの版管理とシャドー比較（issue #872）。
--
-- プロンプトの版は jev_decisions.question_version をそのまま正式な版として使う（列は足さない。理由は
-- docs/architecture/er/tables-market.md §jev_decisions）。この表は、ライブの trader の判定と同じ入力
-- （TraderState・RAG 文脈）を候補の版で判定したシャドーの結果だけを持つ。取引・RAG の索引・Calibration・
-- メタモデルの学習のどれも、この表を読まない（jev_decisions に混ぜないための別表）。
--
-- jev_shadow_decisions
--   jev_decision_id  シャドーが並べた、ライブの jev_decisions 行（decision_type='trader'）。
--   prompt_version   候補の版（ライブの版は jev_decisions.question_version を JOIN で見る）。
--   model_id         応答が返したモデル名。
--   response_json    応答の JSON（TraderResponse）。
--   direction 〜 continuation_probability  各答え（ライブの jev_decisions と同じ意味・範囲）。
--   latency_ms       呼び出しの総所要時間。
--   input_tokens     API が報告した入力トークン数（usage.input_tokens）。
--   request_cost     入力トークン数 × 公表単価（USD）。API は課金額を返さないため、アプリが計算した見積もり。
--   created_at       保存時刻（UTC RFC3339）。費用の日次集計は JST の日で切る。
--   (jev_decision_id, prompt_version) は一意: 1 つのライブ判定に対し、候補の版ごとに 1 行。
--
-- jev_prompt_promotions: ライブの版を候補の版に切り替えた昇格の監査（誰が・いつ・前の版・後の版）。
--   actor  'mcp'（jev_prompt_promote）／'gui'（Settings 画面）。自己改善ループ（Sol/Opus）の経路は無い。
--   client actor='mcp' のときの接続元ホスト（IP のみ）。'gui' は空。
CREATE TABLE jev_shadow_decisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    jev_decision_id INTEGER NOT NULL REFERENCES jev_decisions (id),
    prompt_version VARCHAR(40) NOT NULL CHECK (prompt_version <> ''),
    model_id VARCHAR(100) NOT NULL,
    response_json TEXT NOT NULL,
    direction VARCHAR(10) NOT NULL
        CHECK (direction IN ('LONG', 'SHORT', 'NONE')),
    regime VARCHAR(10) NOT NULL
        CHECK (regime IN ('TREND', 'RANGE', 'BREAKOUT', 'CHAOTIC')),
    entry_quality VARCHAR(12) NOT NULL
        CHECK (entry_quality IN ('poor', 'fair', 'good', 'strong', 'exceptional')),
    confidence NUMERIC(5, 4) NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    toxic_flow NUMERIC(5, 4) NOT NULL CHECK (toxic_flow >= 0 AND toxic_flow <= 1),
    liquidity_stressed NUMERIC(5, 4) NOT NULL CHECK (liquidity_stressed >= 0 AND liquidity_stressed <= 1),
    continuation_probability NUMERIC(5, 4) NOT NULL
        CHECK (continuation_probability >= 0 AND continuation_probability <= 1),
    latency_ms INTEGER NOT NULL,
    input_tokens INTEGER NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    request_cost NUMERIC(10, 6) NOT NULL DEFAULT 0 CHECK (request_cost >= 0),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (jev_decision_id, prompt_version)
);

CREATE INDEX jev_shadow_decisions_created_at_idx ON jev_shadow_decisions (created_at);
CREATE INDEX jev_shadow_decisions_version_idx ON jev_shadow_decisions (prompt_version, jev_decision_id);

CREATE TABLE jev_prompt_promotions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    at TEXT NOT NULL,
    actor TEXT NOT NULL CHECK (actor IN ('mcp', 'gui')),
    client TEXT NOT NULL DEFAULT '',
    from_version VARCHAR(40) NOT NULL CHECK (from_version <> ''),
    to_version VARCHAR(40) NOT NULL CHECK (to_version <> '')
);
