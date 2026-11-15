package datasetexport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// switchReader reads runtime_settings through the read-only q, and only the export switch:
// runtime_settings can hold secret paths, so any other key is refused before SQL runs.
type switchReader struct{ q query.Q }

func (s switchReader) Get(ctx context.Context, key string) (string, bool, error) {
	if key != researchjobsettings.KeyExportEnabled {
		return "", false, fmt.Errorf("datasetexport: runtime setting %q is not readable", key)
	}
	var value string
	err := s.q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("datasetexport: read runtime setting %s: %w", key, err)
	}
	return value, true, nil
}

// Enabled reports whether the export switch (research.export.enabled) is on. Nothing stored,
// or a stored value that does not validate, is off: a fault can never switch the export on.
func Enabled(ctx context.Context, q query.Q) (bool, error) {
	raw, ok, err := switchReader{q}.Get(ctx, researchjobsettings.KeyExportEnabled)
	if err != nil || !ok {
		return false, err
	}
	if researchjobsettings.Validate(researchjobsettings.KeyExportEnabled, raw) != nil {
		return false, nil
	}
	var n int
	_ = json.Unmarshal([]byte(raw), &n) // Validate accepted a whole number
	return n == 1, nil
}
