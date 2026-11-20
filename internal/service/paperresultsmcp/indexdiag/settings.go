package indexdiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/researchindexsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/researchindex"
)

// settingsReader is a read-only runtime_settings view limited to the two
// research.index.* keys: runtime_settings can hold secret paths, so any other key
// is refused before SQL runs. It is the Store researchindexsettings loads from, so
// the reported state is the recorder's own reading.
type settingsReader struct{ q query.Q }

func (s settingsReader) Get(ctx context.Context, key string) (string, bool, error) {
	if !researchindexsettings.IsKey(key) {
		return "", false, fmt.Errorf("indexdiag: runtime setting %q is not readable", key)
	}
	var value string
	err := s.q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("indexdiag: read runtime setting %s: %w", key, err)
	}
	return value, true, nil
}

// loadRecording reports the recorder's lookback (researchindex.DefaultLookbackDays,
// the recorder's own constant): a day older than that which was never checked
// stays missing. It is recording.lookback_days.
func loadRecording(ctx context.Context, q query.Q) (Recording, error) {
	s, err := researchindexsettings.Load(ctx, settingsReader{q})
	if err != nil {
		return Recording{}, fmt.Errorf("indexdiag: %w", err)
	}
	return Recording{Enabled: s.Enabled, Symbols: s.Symbols, LookbackDays: researchindex.DefaultLookbackDays}, nil
}
