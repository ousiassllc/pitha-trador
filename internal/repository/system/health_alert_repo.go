package system

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// HealthAlertRepository persists the active health alerts (health_alerts, one
// row per fixed code) and the detector's last check (health_alert_checks, one
// row), written by the app and read by the Paper results MCP (issue #802).
type HealthAlertRepository struct {
	db *sql.DB
}

// NewHealthAlertRepository returns a HealthAlertRepository backed by db.
func NewHealthAlertRepository(db *sql.DB) *HealthAlertRepository {
	return &HealthAlertRepository{db: db}
}

// Sync stores alerts as the complete active set and now as the last check, in
// one transaction: rows whose code is not in alerts are deleted, the others are
// inserted or updated.
func (r *HealthAlertRepository) Sync(ctx context.Context, now time.Time, alerts []domain.HealthAlert) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repository: begin health alert sync: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	checkedAt := sqlutil.FormatTime(now)
	for _, a := range alerts {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO health_alerts (code, since, checked_at) VALUES (?, ?, ?)
ON CONFLICT(code) DO UPDATE SET since = excluded.since, checked_at = excluded.checked_at`,
			a.Code, sqlutil.FormatTime(a.Since), checkedAt); err != nil {
			return fmt.Errorf("repository: upsert health alert %q: %w", a.Code, err)
		}
	}
	// Every alert still active was just stamped with checkedAt; the rest cleared.
	if _, err = tx.ExecContext(ctx, `DELETE FROM health_alerts WHERE checked_at <> ?`, checkedAt); err != nil {
		return fmt.Errorf("repository: clear cleared health alerts: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO health_alert_checks (id, checked_at) VALUES (1, ?)
ON CONFLICT(id) DO UPDATE SET checked_at = excluded.checked_at`, checkedAt); err != nil {
		return fmt.Errorf("repository: record health alert check: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("repository: commit health alert sync: %w", err)
	}
	return nil
}

// Active returns the stored alerts, oldest first.
func (r *HealthAlertRepository) Active(ctx context.Context) ([]domain.HealthAlert, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT code, since FROM health_alerts ORDER BY since, code`)
	if err != nil {
		return nil, fmt.Errorf("repository: list health alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.HealthAlert
	for rows.Next() {
		var (
			a     domain.HealthAlert
			since string
		)
		if err := rows.Scan(&a.Code, &since); err != nil {
			return nil, fmt.Errorf("repository: scan health alert: %w", err)
		}
		if a.Since, err = sqlutil.ParseTime(since); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list health alerts: %w", err)
	}
	return out, nil
}
