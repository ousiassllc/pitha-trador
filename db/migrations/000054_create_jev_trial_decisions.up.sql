-- jev_trial_decisions: 意思決定モデル比較のお試し記録（issue #1059）。
--
-- ライブの trader の判定（jev_decisions、decision_type='trader'）と同じ入力（TraderState・RAG 文脈）を、
-- 別モデル・別プロバイダー（第 1 段階は OpenRouter 経由の cloudflare/clef-flash・cloudflare/clef・
-- google/gemini-decision-1）に聞いた結果を、成功も失敗も 1 行ずつ残す。jev_decisions にも
-- jev_shadow_decisions にも入れない別表（取引・RAG の索引・Calibration・メタモデルの学習・既存シャドーの
-- どれも、この表を読まない）。(jev_decision_id, arm_id) は一意: 1 つのライブ判定に対し、アームごとに 1 行。
--
--   arm_id            アーム（jev.trial.arms の id。例 or-clef-flash-v3）。#1010 の arm へ写せる文字列 id。
--   prompt_version    聞いた Trader プロンプトの版（登録済みの版）。
--   requested_model   リクエストに送ったモデル id。served_model/served_provider/response_id は応答が返した値
--                     （応答が無い行は空）。
--   status            ok=全問に答えた / failed=応答が得られなかった（通信・HTTP エラー・不正な応答） /
--                     refused=全問を拒否 / partial=一部の質問を拒否。
--   failure_code      status が failed のときの短い分類（http_402 / http_403 / http_429 / http_529 / http_4xx /
--                     http_5xx / timeout / transport / invalid_response）。それ以外は空。
--   http_status       HTTP エラーのときのステータス。それ以外は NULL。
--   error_excerpt     エラーの短い抜粋（全文は残さない。上限 200 バイト）。
--   direction 〜 continuation_probability  各答え。拒否・欠落した質問は NULL（0 や NONE で補わない）。
--   confidence        direction の confidence（モデルが報告しなかったら NULL）。
--   response_json     応答の本文そのまま（応答が無い行は NULL）。生の確率（probabilities）もここに残る。
--   latency_ms        呼び出しの総所要時間。
--   input_tokens / output_tokens  応答の usage。応答が無い・usage が無い行は NULL。
--   cost_usd          報告値 usage.cost。報告が無ければ NULL（0 ではない）。
--   cost_source       reported=usage.cost / list_price_estimate=定価×トークンの推定 / unknown=どちらも無い。
--   counted_cost_usd  1 日の費用上限の判定に数えた額（reported=報告値、list_price_estimate=推定値、
--                     unknown=最悪値側の単価で数えた額）。cost_usd が NULL でもこの列は 0 と限らない。
--                     HTTP エラー応答（課金されない前提）は 0。
--   request_bytes / state_bytes  リクエスト本文・state の大きさ。truncation_suspected はトークン数に対して
--                     リクエストが大きすぎる（入力が切り詰められた疑い）とき 1。Clef 系の切り詰めの有無が
--                     実測できるまで、同じ入力の公平比較とは扱わない。
--   llm_cutoff_status 学習データ終了日の確認状態。既定 unverified（4 モデルとも公表なし。日付を推測で作らない）。
--   rag_includes_live_model_answers  RAG 文脈にライブ Jev の過去の答えが入っていた行は 1（完全な独立条件では
--                     ない）。RAG を送った行は 1。
--   created_at        保存時刻（UTC RFC3339）。費用の日次集計は JST の日で切る。
CREATE TABLE jev_trial_decisions (
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
    cost_source VARCHAR(20) NOT NULL CHECK (cost_source IN ('reported', 'list_price_estimate', 'unknown')),
    counted_cost_usd NUMERIC(12, 8) NOT NULL DEFAULT 0 CHECK (counted_cost_usd >= 0),
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

CREATE INDEX jev_trial_decisions_created_at_idx ON jev_trial_decisions (created_at);
CREATE INDEX jev_trial_decisions_arm_idx ON jev_trial_decisions (arm_id, created_at);
