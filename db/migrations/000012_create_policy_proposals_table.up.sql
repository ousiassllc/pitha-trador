-- policy_proposals: Sol（Think）が生成しOpus（Govern）がレビューする、
-- Policy Engineしきい値の自己改善提案・審査・適用履歴
-- (docs/architecture/er.md §policy_proposals, functional.md §4.14)
CREATE TABLE policy_proposals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    proposed_at TEXT NOT NULL,
    proposed_by VARCHAR(20) NOT NULL DEFAULT 'sol',
    rationale_json TEXT NOT NULL,
    proposed_changes_json TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected', 'applied', 'rolled_back')),
    backtest_result_json TEXT,
    reviewed_by VARCHAR(20) DEFAULT 'opus',
    review_json TEXT,
    applied_policy_version VARCHAR(20),
    applied_at TEXT,
    rolled_back_at TEXT,
    rolled_back_reason VARCHAR(255),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX policy_proposals_status_idx ON policy_proposals (status);
CREATE INDEX policy_proposals_proposed_at_idx ON policy_proposals (proposed_at DESC);
