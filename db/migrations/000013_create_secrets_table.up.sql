-- secrets: JEV_API_KEY/JEV_BASE_URL/KABU_API_PASSWORD/SLACK_WEBHOOK_URL
-- entered via the Settings screen (`/settings`, internal/web/handler.
-- SettingsHandler), replacing the `.env`/environment-variable input path
-- issue #43 originally used (removed by issue #57). Values are encrypted
-- at rest by internal/repository.SecretsRepository using
-- internal/config.EncryptSecret (AES-256-GCM, app-embedded key) - this
-- protects only against the SQLite file itself being copied/shared in
-- plaintext, not against an attacker who can run the compiled binary
-- (see config.EncryptSecret's doc comment for the full threat model).
CREATE TABLE secrets (
    key VARCHAR(100) PRIMARY KEY,
    encrypted_value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
