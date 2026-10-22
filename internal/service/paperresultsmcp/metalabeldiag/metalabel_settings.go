package metalabeldiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// metalabelSettingsReader is a read-only runtime_settings view limited to the
// metalabel.* keys (metalabelsettings.Keys). runtime_settings can hold secret
// paths, so any other key is refused before SQL runs. It is the Store
// metalabelsettings.Load reads, so the effective values are the app's own.
type metalabelSettingsReader struct{ q query.Q }

func (s metalabelSettingsReader) Get(ctx context.Context, key string) (string, bool, error) {
	if !slices.Contains(metalabelsettings.Keys(), key) {
		return "", false, fmt.Errorf("query: runtime setting %q is not readable", key)
	}
	var value string
	err := s.q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query: runtime setting %s: %w", key, err)
	}
	return value, true, nil
}

// loadMetalabelSettings is the effective meta-labeling configuration, read the
// way the nightly retraining reads it.
func loadMetalabelSettings(ctx context.Context, q query.Q) (metalabelsettings.Settings, error) {
	s, err := metalabelsettings.Load(ctx, metalabelSettingsReader{q: q})
	if err != nil {
		return metalabelsettings.Settings{}, fmt.Errorf("query: metalabel settings: %w", err)
	}
	return s, nil
}
