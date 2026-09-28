package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// SecretsRepository persists secrets table rows: JEV_API_KEY/
// JEV_BASE_URL/KABU_API_PASSWORD/SLACK_WEBHOOK_URL entered via the
// Settings screen (`/settings`, internal/web/handler.SettingsHandler),
// replacing the `.env`/environment-variable input path issue #43
// originally used (removed entirely by issue #57).
//
// Values are encrypted at rest with config.EncryptSecret/DecryptSecret
// (AES-256-GCM, app-embedded key - see EncryptSecret's doc comment for
// exactly what threat this does and does not protect against); callers
// of Get/Set only ever see plaintext, mirroring
// RuntimeSettingsRepository's Get/Set shape.
type SecretsRepository struct {
	db *sql.DB
}

// NewSecretsRepository returns a SecretsRepository backed by db.
func NewSecretsRepository(db *sql.DB) *SecretsRepository {
	return &SecretsRepository{db: db}
}

// Get returns key's decrypted plaintext value, or ("", false, nil) when
// no row exists for it yet (config.LoadSecretsFromDB's SecretsRepository
// interface).
func (r *SecretsRepository) Get(ctx context.Context, key string) (string, bool, error) {
	var encrypted string
	err := r.db.QueryRowContext(ctx, `SELECT encrypted_value FROM secrets WHERE key = ?`, key).Scan(&encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("repository: get secret %q: %w", key, err)
	}
	plaintext, err := config.DecryptSecret(encrypted)
	if err != nil {
		return "", false, fmt.Errorf("repository: decrypt secret %q: %w", key, err)
	}
	return plaintext, true, nil
}

// Set encrypts plaintext and upserts it under key, stamping updated_at.
// An empty plaintext clears the stored value (deletes the row) instead
// of persisting an encrypted empty string, so a subsequent Get reports
// it as unset again - the Settings screen's chosen behavior for a field
// submitted blank (internal/web/pages.SettingsPage's doc comment records
// this as the one the operator sees on the form itself).
func (r *SecretsRepository) Set(ctx context.Context, key, plaintext string) error {
	if plaintext == "" {
		if _, err := r.db.ExecContext(ctx, `DELETE FROM secrets WHERE key = ?`, key); err != nil {
			return fmt.Errorf("repository: clear secret %q: %w", key, err)
		}
		return nil
	}

	encrypted, err := config.EncryptSecret(plaintext)
	if err != nil {
		return fmt.Errorf("repository: encrypt secret %q: %w", key, err)
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO secrets (key, encrypted_value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET encrypted_value = excluded.encrypted_value, updated_at = excluded.updated_at`,
		key, encrypted, formatTime(time.Now()))
	if err != nil {
		return fmt.Errorf("repository: set secret %q: %w", key, err)
	}
	return nil
}
