package replaytool

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/jevreplaysettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
)

// rowQuerier is the one-row read the settings reader needs; *sql.Tx and query.Q
// satisfy it.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// settingsReader reads the jev.trial.* and jev.replay.* runtime_settings keys
// only, with the same single-key statement the other fixed-key reads use: no
// other key (and so no secret) is readable through it.
type settingsReader struct{ q rowQuerier }

func (s settingsReader) Get(ctx context.Context, key string) (string, bool, error) {
	if !jevtrialsettings.IsKey(key) && !jevreplaysettings.IsKey(key) {
		return "", false, fmt.Errorf("replaytool: runtime setting %q is not readable", key)
	}
	var value string
	err := s.q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("replaytool: runtime setting %s: %w", key, err)
	}
	return value, true, nil
}
