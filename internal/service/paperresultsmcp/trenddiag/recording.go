package trenddiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/evalrecord"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// settingsReader is a read-only runtime_settings view limited to the two keys of the
// evaluation record (research.eval.enabled and research.eval.retention_days):
// runtime_settings can hold secret paths, so any other key is refused before SQL runs. It
// is the Store researchjobsettings.Eval loads from, so the effective values (and the
// defaults for a missing or damaged row) are the app's own.
type settingsReader struct{ q query.Q }

func (s settingsReader) Get(ctx context.Context, key string) (string, bool, error) {
	if key != researchjobsettings.KeyEvalEnabled && key != researchjobsettings.KeyEvalRetentionDays {
		return "", false, fmt.Errorf("trenddiag: runtime setting %q is not readable", key)
	}
	var value string
	err := s.q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("trenddiag: read runtime setting %s: %w", key, err)
	}
	return value, true, nil
}

// recordingOf reads what the record holds overall (not only the period) and the two
// settings in force now.
func recordingOf(ctx context.Context, q query.Q) (Recording, error) {
	job, err := researchjobsettings.Eval(ctx, settingsReader{q})
	if err != nil {
		return Recording{}, fmt.Errorf("trenddiag: %w", err)
	}
	span, err := evalrecord.RecordedSpan(ctx, q)
	if err != nil {
		return Recording{}, err
	}
	rec := Recording{Enabled: job.Enabled, RetentionDays: job.RetentionDays, DaysRecorded: span.Days}
	if span.Days > 0 {
		rec.FirstRecordedDay, rec.LastRecordedDay = &span.FirstDay, &span.LastDay
	}
	return rec, nil
}
