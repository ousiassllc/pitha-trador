-- jev_replay_runs / jev_replay_results: リプレイ評価（issue #1085）。
--
-- 保存済みのライブの trader 判断（jev_decisions.state_json）を、お試しのアーム（jev.trial.arms）に後から送り、
-- 別モデルの答えを同じ入力で比べる。お試し記録（jev_trial_decisions、#1059）はこれからの判断時点でしか比べられないが、
-- リプレイは過去の判断時点で比べられる。結果は研究用の別表に残す: jev_decisions・jev_shadow_decisions・
-- jev_trial_decisions には書かない。取引・Risk・RAG の索引・Calibration・メタモデルの学習・既存シャドーのどれも、
-- この 2 表を読まない。実行は既定オフ（jev.replay.enabled。無い・壊れているときはオフ）で、オンにするのは Settings 画面だけ。
--
-- jev_replay_runs: 1 回のリプレイの実行（run）。MCP の replay_start が 1 行入れ（pending）、アプリ内のランナーが拾って進め、
-- replay_stop が stop_requested を 1 にする。
--
--   status            pending=開始待ち / running=実行中（日次上限などで待っている間も running。blocked_reason に理由） /
--                     completed=計画の呼び出しを終えた / stopped=止めた（stop_reason に理由） / failed=始められなかった（stop_reason）。
--   stop_requested    1 のとき、ランナーは新しい呼び出しを出さず status を stopped にする（停止フラグ）。
--   stop_reason       終わった（stopped / failed）理由の短い固定コード（requested / run_cost_cap / payment_required /
--                     key_not_set / no_arms / settings_unreadable）。それ以外は空。
--   blocked_reason    running のあいだ新しい呼び出しを出していない理由の固定コード（switch_off / daily_cap_overall /
--                     daily_cap_arm / throttled / stopped_forbidden / stopped_failure_cap / stopped_payment_required / check_failed）。
--                     動いているときは空。日が変われば自動で続きから再開する。
--   spec_json         実行の指定（期間・判断の種類・件数の上限・アーム）。period_from / period_to は期間の保存形式（判断の timestamp の範囲 [from, to)）。
--   max_rows          対象にする判断の件数の上限（1〜1000）。max_decision_id は見積もり時点の jev_decisions.id の上限で、
--                     見積もり後に増えた判断は対象に入らない（費用の見積もりを超えないため）。
--   arms_json         開始時点のアームの設定（jev.trial.arms のうち選んだもの。キーは含まない）。実行中に設定が変わっても、この版で呼ぶ。
--   estimate_*        開始前の見積もり: 判断の件数・呼び出し数（判断×アーム）・最悪値側の費用の合計（予約と同じ単価で数えた額。
--                     実費の上限の目安で、ランナーはこの額を超えたら止める）・定価での費用の目安（定価が分からないアームがあれば NULL）。
--                     estimate_token は見積もりの内容のハッシュで、開始時に同じ内容かを確かめる（見積もりを経ない開始を許さない）。
--   calls_done        記録した結果の行数（失敗を含む）。calls_failed はそのうち status=failed の行数。
--   counted_cost_usd  記録した行の counted_cost_usd の合計（この run が上限の判定に数えた額）。
--
-- jev_replay_results: (run, ライブの判断, アーム) ごとの結果。jev_trial_decisions と同じ答え・費用・状態の列に、
-- run_id と、学習カットオフの確認状態と参考扱いの印を足したもの。
--
--   llm_cutoff_status verified=アームに学習カットオフの日付（llm_cutoff）があり、その日より後の判断だけを送った /
--                     unverified=日付が公表されていない（画面では「未確認」）。日付を推測で入れない。
--   reference_only    1=参考扱い（カットオフが未確認で、学習データに入っていた可能性を排除できない）。unverified の行は必ず 1。
--   rag_includes_live_model_answers  リプレイは過去の類似事例（RAG）を足さない（足すと判断より後のデータが入る）ので常に 0。
--   request_bytes     実際に送ったリクエスト本文の大きさ。送った state は jev_decisions.state_json そのままで、何も足さない。
--   その他の列は jev_trial_decisions と同じ意味（docs/architecture/er/tables-jev-trial.md）。
--   created_at        保存時刻（UTC RFC3339 の固定幅）。費用の日次集計は JST の日で切り、お試しの費用と合算して上限と比べる。
CREATE TABLE jev_replay_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    status VARCHAR(10) NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'stopped', 'failed')),
    stop_requested INTEGER NOT NULL DEFAULT 0 CHECK (stop_requested IN (0, 1)),
    stop_reason VARCHAR(32) NOT NULL DEFAULT '',
    blocked_reason VARCHAR(32) NOT NULL DEFAULT '',
    spec_json TEXT NOT NULL,
    period_from TEXT NOT NULL,
    period_to TEXT NOT NULL,
    max_rows INTEGER NOT NULL CHECK (max_rows >= 1 AND max_rows <= 1000),
    max_decision_id INTEGER NOT NULL CHECK (max_decision_id >= 0),
    arms_json TEXT NOT NULL,
    estimate_decisions INTEGER NOT NULL CHECK (estimate_decisions >= 0),
    estimate_calls INTEGER NOT NULL CHECK (estimate_calls >= 0),
    estimate_max_cost_usd NUMERIC(12, 8) NOT NULL CHECK (estimate_max_cost_usd >= 0),
    estimate_list_price_usd NUMERIC(12, 8) CHECK (estimate_list_price_usd IS NULL OR estimate_list_price_usd >= 0),
    estimate_token VARCHAR(64) NOT NULL CHECK (estimate_token <> ''),
    calls_done INTEGER NOT NULL DEFAULT 0 CHECK (calls_done >= 0),
    calls_failed INTEGER NOT NULL DEFAULT 0 CHECK (calls_failed >= 0),
    counted_cost_usd NUMERIC(12, 8) NOT NULL DEFAULT 0 CHECK (counted_cost_usd >= 0),
    created_by VARCHAR(64) NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    updated_at TEXT NOT NULL,
    CHECK ((status IN ('completed', 'stopped', 'failed')) = (finished_at IS NOT NULL))
);

CREATE INDEX jev_replay_runs_status_idx ON jev_replay_runs (status, id);

CREATE TABLE jev_replay_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL REFERENCES jev_replay_runs (id),
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
    llm_cutoff_status VARCHAR(20) NOT NULL CHECK (llm_cutoff_status IN ('verified', 'unverified')),
    reference_only INTEGER NOT NULL CHECK (reference_only IN (0, 1)),
    rag_includes_live_model_answers INTEGER NOT NULL DEFAULT 0 CHECK (rag_includes_live_model_answers IN (0, 1)),
    created_at TEXT NOT NULL,
    UNIQUE (run_id, jev_decision_id, arm_id),
    CHECK (status <> 'ok' OR (direction IS NOT NULL AND regime IS NOT NULL AND entry_quality IS NOT NULL
        AND toxic_flow IS NOT NULL AND liquidity_stressed IS NOT NULL AND continuation_probability IS NOT NULL)),
    CHECK ((status = 'failed') = (failure_code <> '')),
    CHECK (llm_cutoff_status = 'verified' OR reference_only = 1)
);

CREATE INDEX jev_replay_results_created_at_idx ON jev_replay_results (created_at);
CREATE INDEX jev_replay_results_arm_idx ON jev_replay_results (arm_id, created_at);
CREATE INDEX jev_replay_results_decision_idx ON jev_replay_results (jev_decision_id);
