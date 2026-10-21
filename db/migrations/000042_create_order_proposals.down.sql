-- 注文案の 2 表を消し、mcp_settings_audit の action を 000040 の 4 値へ戻す。
-- 受け付けスイッチの切替の行（order_proposals_enable / order_proposals_disable）は戻した CHECK に入らないので捨てる。
DROP TABLE order_proposal_events;
DROP TABLE order_proposals;

CREATE TABLE mcp_settings_audit_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    at TEXT NOT NULL,
    actor TEXT NOT NULL CHECK (actor IN ('mcp', 'gui')),
    client TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL CHECK (action IN ('set', 'reset', 'write_enable', 'write_disable')),
    setting_key TEXT NOT NULL CHECK (setting_key <> ''),
    before_value TEXT,
    after_value TEXT
);

INSERT INTO mcp_settings_audit_old (id, at, actor, client, action, setting_key, before_value, after_value)
SELECT id, at, actor, client, action, setting_key, before_value, after_value FROM mcp_settings_audit
WHERE action IN ('set', 'reset', 'write_enable', 'write_disable');

DROP TABLE mcp_settings_audit;
ALTER TABLE mcp_settings_audit_old RENAME TO mcp_settings_audit;

CREATE INDEX mcp_settings_audit_key_idx ON mcp_settings_audit (setting_key, id);
