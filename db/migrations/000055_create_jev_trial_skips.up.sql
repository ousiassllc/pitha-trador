-- jev_trial_skips: お試し（意思決定モデル比較）が、ライブの判断に対してアームを「呼ばなかった」記録（issue #1062）。
--
-- ライブの trader の判断ごと・アームごとに、比較を見送った理由を 1 行だけ残す。見送った判断は
-- jev_trial_decisions に行を残さないので、比較（jev_trial_compare）の cohort（選んだアームすべてに聞いた判断）から
-- 静かに抜ける。この表はその抜けた件数を理由別に数えるための材料で、呼び出していない判断を成功や失敗の行
-- としては扱わない（jev_trial_decisions には入れない別表。答え・費用・status を持たない）。取引・RAG の索引・
-- Calibration・メタモデルの学習・既存シャドーのどれも、この表を読まない。(jev_decision_id, arm_id) は一意。
--
--   arm_id   見送ったアーム（jev.trial.arms の id）。
--   reason   見送りの理由の固定コード（コード側が検証する。値の追加で移行は要らない）:
--            concurrency_full / min_interval / daily_cap_overall / daily_cap_arm / key_not_set /
--            prompt_version_unregistered / stopped_payment_required / stopped_forbidden /
--            stopped_failure_cap / stopped_other / throttled / check_failed。
--   created_at  見送りを決めた時刻（UTC）。
CREATE TABLE jev_trial_skips (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    jev_decision_id INTEGER NOT NULL REFERENCES jev_decisions (id),
    arm_id VARCHAR(48) NOT NULL CHECK (arm_id <> ''),
    reason VARCHAR(40) NOT NULL CHECK (reason <> ''),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (jev_decision_id, arm_id)
);

CREATE INDEX jev_trial_skips_created_at_idx ON jev_trial_skips (created_at);
