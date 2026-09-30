package system

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// ErrKillSwitchEventNotFound is returned by KillSwitchRepository methods
// when no matching kill_switch_events row exists (Get), or Resolve's
// target row is missing or already resolved.
var ErrKillSwitchEventNotFound = errors.New("repository: kill switch event not found")

// KillSwitchRepository persists kill_switch_events rows: Risk Engine's
// Kill Switch activation/resolution audit log
// (docs/architecture/er.md §kill_switch_events, functional.md FR-RISK-5).
type KillSwitchRepository struct {
	db       *sql.DB
	observer KillSwitchObserver
}

// KillSwitchObserver is notified after every committed Insert with the
// stored row (docs/architecture/overview.md §12). It runs synchronously
// on the writer's goroutine after the write has committed and cannot fail
// the write, so it must return quickly.
type KillSwitchObserver func(ctx context.Context, ev domain.KillSwitchEvent)

// SetObserver registers fn to be called after every committed Insert,
// replacing any previous observer. It is not safe to call concurrently
// with other KillSwitchRepository methods; wire it once during
// composition.
func (r *KillSwitchRepository) SetObserver(fn KillSwitchObserver) { r.observer = fn }

// NewKillSwitchRepository returns a KillSwitchRepository backed by db.
func NewKillSwitchRepository(db *sql.DB) *KillSwitchRepository {
	return &KillSwitchRepository{db: db}
}

const insertKillSwitchEventSQL = `
INSERT INTO kill_switch_events (triggered_at, reason, detail_json, created_at)
VALUES (?, ?, ?, ?)`

// Insert writes a single, not-yet-resolved kill_switch_events row.
// ev.TriggeredAt defaults to ev.CreatedAt (and that, in turn, to now) when
// zero, so callers may leave both unset.
func (r *KillSwitchRepository) Insert(ctx context.Context, ev domain.KillSwitchEvent) (domain.KillSwitchEvent, error) {
	createdAt := ev.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	triggeredAt := ev.TriggeredAt
	if triggeredAt.IsZero() {
		triggeredAt = createdAt
	}

	res, err := r.db.ExecContext(ctx, insertKillSwitchEventSQL,
		sqlutil.FormatTime(triggeredAt), ev.Reason, ev.DetailJSON, sqlutil.FormatTime(createdAt))
	if err != nil {
		return domain.KillSwitchEvent{}, fmt.Errorf("repository: insert kill switch event (reason=%s): %w", ev.Reason, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.KillSwitchEvent{}, fmt.Errorf("repository: read kill switch event id (reason=%s): %w", ev.Reason, err)
	}

	ev.ID = id
	ev.TriggeredAt = triggeredAt
	ev.CreatedAt = createdAt
	ev.ResolvedAt = nil
	ev.ResolvedBy = nil
	if r.observer != nil {
		r.observer(ctx, ev)
	}
	return ev, nil
}

// ListRecent returns up to limit kill_switch_events rows, most recently
// triggered first, resolved or not - System Activity Log's Kill Switch
// feed.
func (r *KillSwitchRepository) ListRecent(ctx context.Context, limit int) ([]domain.KillSwitchEvent, error) {
	rows, err := r.db.QueryContext(ctx,
		killSwitchSelectColumns+` ORDER BY e.triggered_at DESC, e.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: list recent kill switch events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.KillSwitchEvent
	for rows.Next() {
		ev, err := scanKillSwitchEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list recent kill switch events: %w", err)
	}
	return out, nil
}

// killSwitchSelectColumns joins each event with its (at most one)
// kill_switch_resolutions row, so resolved_at/resolved_by are NULL while
// the event is still unresolved. Callers append WHERE/ORDER BY clauses
// qualified with the e./r. aliases.
const killSwitchSelectColumns = `
SELECT e.id, e.triggered_at, e.reason, e.detail_json, r.resolved_at, r.resolved_by, e.created_at
FROM kill_switch_events e
LEFT JOIN kill_switch_resolutions r ON r.kill_switch_event_id = e.id`

// Get returns the kill_switch_events row with the given id.
func (r *KillSwitchRepository) Get(ctx context.Context, id int64) (domain.KillSwitchEvent, error) {
	row := r.db.QueryRowContext(ctx, killSwitchSelectColumns+` WHERE e.id = ?`, id)
	return scanKillSwitchEvent(row)
}

// ListUnresolved returns every kill_switch_events row not yet resolved
// (resolved_at IS NULL), most recently triggered first. An empty/nil
// result means the system is not currently Kill-Switched.
func (r *KillSwitchRepository) ListUnresolved(ctx context.Context) ([]domain.KillSwitchEvent, error) {
	rows, err := r.db.QueryContext(ctx,
		killSwitchSelectColumns+` WHERE r.id IS NULL ORDER BY e.triggered_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("repository: list unresolved kill switch events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.KillSwitchEvent
	for rows.Next() {
		ev, err := scanKillSwitchEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list unresolved kill switch events: %w", err)
	}
	return out, nil
}

// Resolve records that the still-unresolved kill_switch_events row id was
// resolved at resolvedAt by resolvedBy (domain.ResolvedByAuto or
// domain.ResolvedByManual). kill_switch_events is append-only (migration
// 000014), so the resolution is appended to kill_switch_resolutions rather
// than updating the event row. It returns ErrKillSwitchEventNotFound when
// id does not exist or was already resolved.
func (r *KillSwitchRepository) Resolve(ctx context.Context, id int64, resolvedAt time.Time, resolvedBy string) error {
	res, err := r.db.ExecContext(ctx, `
INSERT INTO kill_switch_resolutions (kill_switch_event_id, resolved_at, resolved_by)
SELECT id, ?, ? FROM kill_switch_events WHERE id = ?
ON CONFLICT (kill_switch_event_id) DO NOTHING`,
		sqlutil.FormatTime(resolvedAt), resolvedBy, id)
	if err != nil {
		return fmt.Errorf("repository: resolve kill switch event %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: read resolve result for kill switch event %d: %w", id, err)
	}
	if n == 0 {
		return ErrKillSwitchEventNotFound
	}
	return nil
}

func scanKillSwitchEvent(row sqlutil.RowScanner) (domain.KillSwitchEvent, error) {
	var (
		ev          domain.KillSwitchEvent
		triggeredAt string
		resolvedAt  sql.NullString
		resolvedBy  sql.NullString
		createdAt   string
	)
	err := row.Scan(&ev.ID, &triggeredAt, &ev.Reason, &ev.DetailJSON, &resolvedAt, &resolvedBy, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.KillSwitchEvent{}, ErrKillSwitchEventNotFound
	}
	if err != nil {
		return domain.KillSwitchEvent{}, fmt.Errorf("repository: scan kill switch event: %w", err)
	}

	if ev.TriggeredAt, err = sqlutil.ParseTime(triggeredAt); err != nil {
		return domain.KillSwitchEvent{}, err
	}
	if ev.CreatedAt, err = sqlutil.ParseTime(createdAt); err != nil {
		return domain.KillSwitchEvent{}, err
	}
	if ev.ResolvedAt, err = sqlutil.ParseNullableTime(resolvedAt); err != nil {
		return domain.KillSwitchEvent{}, err
	}
	if resolvedBy.Valid {
		by := resolvedBy.String
		ev.ResolvedBy = &by
	}
	return ev, nil
}
