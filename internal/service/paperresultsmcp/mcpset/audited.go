package mcpset

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

// Audited is the Settings screen's opsettings.Service with an audit trail:
// Save and Reset of a key the MCP may write (mcpwritesettings) are recorded in
// mcp_settings_audit as GUI changes, so settings_diff and settings_audit see
// every change of those keys whoever made it (issue #806). Everything else is
// the embedded service unchanged, and changes of other keys are not recorded.
type Audited struct {
	*opsettings.Service
	gate *Gate
}

// NewAudited wraps svc, recording through gate.
func NewAudited(svc *opsettings.Service, gate *Gate) *Audited {
	return &Audited{Service: svc, gate: gate}
}

// Save stores raw for key (see opsettings.Service.Save) and records the change.
func (a *Audited) Save(ctx context.Context, key, raw string) error {
	return a.audited(ctx, key, false, func(svc *opsettings.Service) error { return svc.Save(ctx, key, raw) })
}

// Reset removes key's stored value (see opsettings.Service.Reset) and records
// the change; a reset of a key with nothing stored changes nothing and is not
// recorded.
func (a *Audited) Reset(ctx context.Context, key string) error {
	return a.audited(ctx, key, true, func(svc *opsettings.Service) error { return svc.Reset(ctx, key) })
}

// audited runs change and records it in one transaction (issue #813): the
// setting and its audit row are stored together, and when either fails both
// are rolled back and the error is returned. change gets a service over that
// transaction.
//
// One case stays unaudited: a stored value that cannot be read before the
// change (a damaged row) must not stop the operator from overwriting or
// resetting it from the Settings screen, so it is logged and changed as is.
func (a *Audited) audited(ctx context.Context, key string, reset bool, change func(*opsettings.Service) error) error {
	if !a.gate.Watches(key) {
		return change(a.Service)
	}
	return a.gate.inTx(ctx, func(tx *sql.Tx, now time.Time) error {
		svc := a.WithStore(system.NewRuntimeSettingsRepositoryOn(tx))
		before, err := svc.Get(ctx, key)
		if err != nil {
			slog.WarnContext(ctx, "mcpset: audit: read value before change", "key", key, "error", err)
			return change(svc)
		}
		if err := change(svc); err != nil {
			return err
		}
		after, err := svc.Get(ctx, key)
		if err != nil {
			return err
		}
		return recordOn(ctx, tx, now, key, before, after, reset)
	})
}
