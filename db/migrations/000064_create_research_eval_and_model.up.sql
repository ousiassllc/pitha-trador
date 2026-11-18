-- 研究専用の 3 表（issue #1089・#1092。列の意味は docs/architecture/er/tables-research.md）。取引・Risk・
-- 発注・Jev・メタモデルは読まない。既存の表には列も制約もトリガも足さない。記録・学習はどちらもオフ既定。
CREATE TABLE jev_evaluation_daily (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    day TEXT NOT NULL CHECK (length(day) = 10),
    model_id VARCHAR(100) NOT NULL,
    question_version VARCHAR(20) NOT NULL,
    horizon_minutes INTEGER NOT NULL CHECK (horizon_minutes > 0),
    take_profit_bps NUMERIC NOT NULL, stop_loss_bps NUMERIC NOT NULL, barrier_cost_bps NUMERIC NOT NULL,
    decisions INTEGER NOT NULL CHECK (decisions >= 0),
    evaluated INTEGER NOT NULL CHECK (evaluated >= 0 AND evaluated <= decisions),
    entered INTEGER NOT NULL CHECK (entered >= 0 AND entered <= evaluated),
    wins INTEGER NOT NULL CHECK (wins >= 0 AND wins <= evaluated),
    llm_sum_bps NUMERIC NOT NULL, random_sum_bps NUMERIC NOT NULL, momentum_sum_bps NUMERIC NOT NULL, contrarian_sum_bps NUMERIC NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (day, model_id, question_version, horizon_minutes)
);
CREATE INDEX jev_evaluation_daily_day_idx ON jev_evaluation_daily (day);
CREATE TABLE research_model_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    version VARCHAR(80) NOT NULL UNIQUE CHECK (version <> ''),
    algorithm VARCHAR(20) NOT NULL CHECK (algorithm <> ''),
    label_horizon TEXT NOT NULL CHECK (label_horizon IN ('30m', '60m', 'close')),
    features_json TEXT NOT NULL, hyperparams_json TEXT NOT NULL, coefficients_json TEXT NOT NULL,
    train_from TEXT NOT NULL CHECK (length(train_from) = 10), train_to TEXT NOT NULL CHECK (length(train_to) = 10),
    predict_day TEXT NOT NULL CHECK (length(predict_day) = 10 AND train_to < predict_day),
    n_train INTEGER NOT NULL CHECK (n_train > 0), n_predicted INTEGER NOT NULL CHECK (n_predicted >= 0),
    train_digest VARCHAR(64) NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE TABLE research_model_predictions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL,
    jev_decision_id INTEGER NOT NULL,
    instrument_id INTEGER NOT NULL,
    snapshot_at TEXT NOT NULL,
    day TEXT NOT NULL CHECK (length(day) = 10),
    pred_long_net_bps NUMERIC NOT NULL, pred_short_net_bps NUMERIC NOT NULL,
    direction VARCHAR(5) NOT NULL CHECK (direction IN ('LONG', 'SHORT', 'NONE')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (run_id, jev_decision_id)
);
CREATE INDEX research_model_predictions_decision_idx ON research_model_predictions (jev_decision_id);
CREATE INDEX research_model_predictions_day_idx ON research_model_predictions (day);
