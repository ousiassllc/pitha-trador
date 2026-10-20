package mcpset

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// ErrReadOnly is returned by ReadStore's Set and Delete: a read model can
// never write.
var ErrReadOnly = errors.New("mcpset: read-only store")

// ReadStore is opsettings.Store over a read-only query handle: Get reads
// runtime_settings, Set and Delete refuse. It lets the read models reuse
// opsettings' effective-value logic on the MCP's mode=ro connection.
type ReadStore struct{ Q query.Q }

// Get implements opsettings.Store.
func (s ReadStore) Get(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.Q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("mcpset: read runtime setting %q: %w", key, err)
	}
	return value, true, nil
}

// Set implements opsettings.Store; it always fails.
func (ReadStore) Set(context.Context, string, string, time.Time) error { return ErrReadOnly }

// Delete implements opsettings.Store; it always fails.
func (ReadStore) Delete(context.Context, string) error { return ErrReadOnly }

// newService is the opsettings.Service the settings tools use: only the
// whitelisted keys are ever passed to it. base is config/risk.yaml's Paper
// section (the default of the Paper keys while nothing is stored); nil uses
// the issue #804 initial values.
func newService(store opsettings.Store, base *config.RiskLimits) *opsettings.Service {
	svc := opsettings.New(store, config.StrategyConfig{}, "")
	if base != nil {
		svc.WithRiskReference(*base)
	}
	return svc
}

// WriteEnabled reports the write switch: the stored 0/1, or
// mcpwritesettings.DefaultWriteEnabled (on) while nothing is stored. A stored
// value that is neither 0 nor 1 reads as off (fail closed).
func WriteEnabled(ctx context.Context, store interface {
	Get(ctx context.Context, key string) (string, bool, error)
}) (bool, error) {
	raw, ok, err := store.Get(ctx, mcpwritesettings.KeyWriteEnabled)
	if err != nil {
		return false, err
	}
	if !ok {
		return mcpwritesettings.DefaultWriteEnabled, nil
	}
	on, err := mcpwritesettings.Apply(raw)
	return on && err == nil, nil
}

// PaperCapitalJPY returns the Paper capital in force: the runtime_settings
// value of risk.paper.initial_capital_jpy (the Settings value, issue #804)
// when a valid one is stored, otherwise fallback (config/risk.yaml's). The
// MCP's return-rate calculations use it, read-only.
func PaperCapitalJPY(ctx context.Context, q query.Q, fallback float64) (float64, error) {
	raw, ok, err := ReadStore{Q: q}.Get(ctx, papersettings.KeyInitialCapitalJPY)
	if err != nil || !ok {
		return fallback, err
	}
	var o papersettings.Overrides
	if papersettings.Apply(&o, papersettings.KeyInitialCapitalJPY, raw) != nil || o.InitialCapitalJPY == nil {
		return fallback, nil // a damaged row is ignored, like the Risk Engine does
	}
	return *o.InitialCapitalJPY, nil
}
