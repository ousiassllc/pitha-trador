package system

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// RuntimeSettingsRepository persists runtime_settings rows: the
// key/value store overriding config/*.yaml defaults without a redeploy,
// and (for `system.*` keys) single high-frequency-update values such as
// the operator heartbeat timestamp (docs/architecture/er.md
// §runtime_settings, functional.md FR-RISK-6).
//
// Values are stored verbatim as the caller's JSON-encoded string (er.md:
// "value | text | NOT NULL | JSON文字列"); this repository does not
// interpret or validate them.
type RuntimeSettingsRepository struct {
	db *sql.DB
}

// NewRuntimeSettingsRepository returns a RuntimeSettingsRepository backed
// by db.
func NewRuntimeSettingsRepository(db *sql.DB) *RuntimeSettingsRepository {
	return &RuntimeSettingsRepository{db: db}
}

// Get returns key's current JSON-encoded value, or ("", false, nil) when
// no row exists for it yet.
func (r *RuntimeSettingsRepository) Get(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("repository: get runtime setting %q: %w", key, err)
	}
	return value, true, nil
}

// Set upserts key to value (a JSON-encoded string), stamping updatedAt.
func (r *RuntimeSettingsRepository) Set(ctx context.Context, key, value string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO runtime_settings (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, sqlutil.FormatTime(updatedAt))
	if err != nil {
		return fmt.Errorf("repository: set runtime setting %q: %w", key, err)
	}
	return nil
}
