package fxdiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/researchfxsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// settingsReader is a read-only runtime_settings view limited to the two
// research.fx.* keys: runtime_settings can hold secret paths, so any other key is
// refused before SQL runs. It is the Store researchfxsettings loads from, so the
// reported state is the recorder's own reading.
type settingsReader struct{ q query.Q }

func (s settingsReader) Get(ctx context.Context, key string) (string, bool, error) {
	if !researchfxsettings.IsKey(key) {
		return "", false, fmt.Errorf("fxdiag: runtime setting %q is not readable", key)
	}
	var value string
	err := s.q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("fxdiag: read runtime setting %s: %w", key, err)
	}
	return value, true, nil
}

func loadRecording(ctx context.Context, q query.Q) (Recording, error) {
	s, err := researchfxsettings.Load(ctx, settingsReader{q})
	if err != nil {
		return Recording{}, fmt.Errorf("fxdiag: %w", err)
	}
	return Recording{Enabled: s.Enabled, Symbols: s.Symbols, LookbackDays: LookbackDays}, nil
}
