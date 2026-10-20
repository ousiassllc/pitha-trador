package mcpset

import (
	"context"
	"log/slog"

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
	return a.audited(ctx, key, false, func() error { return a.Service.Save(ctx, key, raw) })
}

// Reset removes key's stored value (see opsettings.Service.Reset) and records
// the change; a reset of a key with nothing stored changes nothing and is not
// recorded.
func (a *Audited) Reset(ctx context.Context, key string) error {
	return a.audited(ctx, key, true, func() error { return a.Service.Reset(ctx, key) })
}

// audited runs change and, when it succeeded, records it. A failed audit
// write is logged and neither undoes nor fails the saved setting: the setting
// is the source of truth, the log is the trail.
func (a *Audited) audited(ctx context.Context, key string, reset bool, change func() error) error {
	if !a.gate.Watches(key) {
		return change()
	}
	before, err := a.Get(ctx, key)
	if err != nil {
		slog.WarnContext(ctx, "mcpset: audit: read value before change", "key", key, "error", err)
		return change()
	}
	if err := change(); err != nil {
		return err
	}
	after, err := a.Get(ctx, key)
	if err != nil {
		slog.WarnContext(ctx, "mcpset: audit: read value after change", "key", key, "error", err)
		return nil
	}
	if err := a.gate.Record(ctx, key, before, after, reset); err != nil {
		slog.ErrorContext(ctx, "mcpset: audit: record change", "key", key, "error", err)
	}
	return nil
}
