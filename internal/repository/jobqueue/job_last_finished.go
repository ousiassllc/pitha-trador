package jobqueue

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// LastFinishedAt returns the latest finished_at of a succeeded or failed job
// of queue (a terminal skip is a succeeded job), or ok=false when none is
// retained. Each status is one backward step on jobs_queue_status_finished_idx.
func (r *JobRepository) LastFinishedAt(ctx context.Context, queue string) (at time.Time, ok bool, err error) {
	var latest sql.NullString
	err = r.db.QueryRowContext(ctx, `
SELECT MAX(finished) FROM (
	SELECT MAX(finished_at) AS finished FROM jobs WHERE queue = ? AND status = ?
	UNION ALL
	SELECT MAX(finished_at) FROM jobs WHERE queue = ? AND status = ?
)`, queue, JobStatusSucceeded, queue, JobStatusFailed).Scan(&latest)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("repository: last finished job (queue=%q): %w", queue, err)
	}
	if !latest.Valid {
		return time.Time{}, false, nil
	}
	at, err = sqlutil.ParseTime(latest.String)
	if err != nil {
		return time.Time{}, false, err
	}
	return at, true, nil
}
