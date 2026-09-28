-- runtime_settings: Fast Screener/Policy/Riskのしきい値をコード再デプロイ
-- なしで変更するためのKey-Valueストア（docs/architecture/er.md
-- §runtime_settings）
CREATE TABLE runtime_settings (
    key VARCHAR(100) PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
