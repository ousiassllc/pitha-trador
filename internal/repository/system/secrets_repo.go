package system

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// ErrEmptySecret is returned by SecretsRepository.Set for an empty
// plaintext; use Delete to remove a value.
var ErrEmptySecret = errors.New("empty secret value")

// SecretsRepository persists secrets table rows: JEV_API_KEY/
// JEV_BASE_URL/KABU_API_PASSWORD/SLACK_WEBHOOK_URL entered via the
// Settings screen (`/settings`, internal/web/handler/settings.SettingsHandler),
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
	return getSecret(ctx, r.db, key)
}

func getSecret(ctx context.Context, q sqlutil.Executor, key string) (string, bool, error) {
	var encrypted string
	err := q.QueryRowContext(ctx, `SELECT encrypted_value FROM secrets WHERE key = ?`, key).Scan(&encrypted)
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
// An empty plaintext is rejected: removing a value is the explicit Delete
// operation, so a blank input can never wipe a stored secret by accident
// (issue #79). Saving config.KeyJevModel also records the switch in
// jev_model_changes, in the same transaction (issue #885).
func (r *SecretsRepository) Set(ctx context.Context, key, plaintext string) error {
	if plaintext == "" {
		return fmt.Errorf("repository: set secret %q: %w", key, ErrEmptySecret)
	}

	encrypted, err := config.EncryptSecret(plaintext)
	if err != nil {
		return fmt.Errorf("repository: encrypt secret %q: %w", key, err)
	}
	if key == config.KeyJevModel {
		return r.inModelTx(ctx, key, func(tx *sql.Tx, before *string) error {
			if err := upsertSecret(ctx, tx, key, encrypted); err != nil {
				return err
			}
			return recordJevModelChange(ctx, tx, before, &plaintext)
		})
	}
	return upsertSecret(ctx, r.db, key, encrypted)
}

func upsertSecret(ctx context.Context, x sqlutil.Execer, key, encrypted string) error {
	_, err := x.ExecContext(ctx, `
INSERT INTO secrets (key, encrypted_value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET encrypted_value = excluded.encrypted_value, updated_at = excluded.updated_at`,
		key, encrypted, sqlutil.FormatTime(time.Now()))
	if err != nil {
		return fmt.Errorf("repository: set secret %q: %w", key, err)
	}
	return nil
}

// Delete removes key's stored value so a subsequent Get reports it as
// unset. Deleting a key that has no row is not an error. Deleting
// config.KeyJevModel also records the switch back to the default model in
// jev_model_changes (issue #885).
func (r *SecretsRepository) Delete(ctx context.Context, key string) error {
	if key == config.KeyJevModel {
		return r.inModelTx(ctx, key, func(tx *sql.Tx, before *string) error {
			if err := deleteSecret(ctx, tx, key); err != nil {
				return err
			}
			return recordJevModelChange(ctx, tx, before, nil)
		})
	}
	return deleteSecret(ctx, r.db, key)
}

func deleteSecret(ctx context.Context, x sqlutil.Execer, key string) error {
	if _, err := x.ExecContext(ctx, `DELETE FROM secrets WHERE key = ?`, key); err != nil {
		return fmt.Errorf("repository: delete secret %q: %w", key, err)
	}
	return nil
}
