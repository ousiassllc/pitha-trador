// Package jevaudit gives the Settings screen's Jev prompt section an audit
// trail for the Jev shadow's two whitelisted keys (issue #1113):
// jev.shadow.enabled and jev.shadow.candidate_version.
//
// Service wraps jevprompt.Service the way mcpset.Audited wraps
// opsettings.Service: a save or reset of either key writes one row to
// mcp_settings_audit (the table the MCP's settings_set writes) with
// actor=gui, in the same transaction as the setting, so settings_diff,
// settings_audit and an experiment's switch_check see a change made on the
// screen like one made through the MCP. Validation is jevprompt.Service's, run
// unchanged inside that transaction: a rejected save stores neither the setting
// nor an audit row. Everything else (State, the cost cap, Promote) is the
// embedded service as is.
//
// The two keys are not secret, so their values are recorded as they are, in the
// form settings_get / settings_set use ("true" / "false" and the version name).
package jevaudit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jevprompt"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
)

// Service is jevprompt.Service with an audit trail on SetEnabled and
// SetCandidate.
type Service struct {
	*jevprompt.Service
	db  *sql.DB
	now func() time.Time
}

// New wraps svc, which must run over db, recording into db.
func New(svc *jevprompt.Service, db *sql.DB) *Service {
	return &Service{Service: svc, db: db, now: time.Now}
}

// SetClock replaces the clock of the wrapper and of the wrapped service; tests
// use it.
func (s *Service) SetClock(now func() time.Time) {
	s.now = now
	s.Service.SetClock(now)
}

// SetEnabled turns the shadow on or off (see jevprompt.Service.SetEnabled) and
// records the change.
func (s *Service) SetEnabled(ctx context.Context, on bool) error {
	return s.audited(ctx, jevshadowsettings.KeyEnabled, false, func(svc *jevprompt.Service) error { return svc.SetEnabled(ctx, on) })
}

// SetCandidate chooses the candidate version (see
// jevprompt.Service.SetCandidate) and records the change. "" clears the
// candidate: that is the reset to the default, recorded as a reset when a value
// was stored.
func (s *Service) SetCandidate(ctx context.Context, version string) error {
	return s.audited(ctx, jevshadowsettings.KeyCandidateVersion, version == "", func(svc *jevprompt.Service) error { return svc.SetCandidate(ctx, version) })
}

// audited runs change over a transaction and records it there: when change or
// the audit row fails, both are rolled back and the error is returned, so a
// rejected save leaves no trace. A save that leaves the effective value as it
// was (the Settings form sends both keys on every save) is stored but not
// recorded: it is not a change, and switch_check and the confounder check
// would otherwise see a switch that never happened.
func (s *Service) audited(ctx context.Context, key string, reset bool, change func(*jevprompt.Service) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("jevaudit: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	store := system.NewRuntimeSettingsRepositoryOn(tx)
	before, err := mcpset.JevShadowValue(ctx, store, key)
	if err != nil {
		return err
	}
	if err := change(s.WithSettings(store)); err != nil {
		return err
	}
	after, err := mcpset.JevShadowValue(ctx, store, key)
	if err != nil {
		return err
	}
	if before.Current != after.Current || (reset && before.Overridden) {
		if err := mcpset.RecordGUIChange(ctx, tx, s.now(), key, before, after, reset); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("jevaudit: commit transaction: %w", err)
	}
	return nil
}
