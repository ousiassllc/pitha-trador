package jevaudit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset/trialkeys"
)

// Trial is jevtrial.Service with an audit trail on Save (issue #1220): the
// Settings screen's model-comparison trial form stores up to six whitelisted keys
// (jevtrialsettings.OperatorKeys) at once, and each key whose effective value
// changed gets one row in mcp_settings_audit with actor=gui, in the same
// transaction as the settings, so settings_diff, settings_audit and an
// experiment's switch_check see the screen's change like the MCP's. Validation is
// jevtrial.Service's, run unchanged inside that transaction: a rejected form
// stores no setting and no audit row. State is the wrapped service's as is. The
// service's other writers (SetEnabled, SetArms, SetCap, ResetCap) are not exposed,
// so no unaudited path to these keys hangs off Trial.
//
// A blank cap (the form's way to restore a default) removes the stored value; it
// is recorded as a reset when a value was stored. A save that leaves a key's
// effective value as it was (the form sends every field on every save) is stored
// but not recorded: it is not a change.
type Trial struct {
	svc *jevtrial.Service
	db  *sql.DB
	now func() time.Time
}

// NewTrial wraps svc, which must run over db, recording into db.
func NewTrial(svc *jevtrial.Service, db *sql.DB) *Trial {
	return &Trial{svc: svc, db: db, now: time.Now}
}

// SetClock replaces the clock of the wrapper and of the wrapped service; tests
// use it.
func (t *Trial) SetClock(now func() time.Time) {
	t.now = now
	t.svc.SetClock(now)
}

// State reads the trial's state for the Settings panel (see jevtrial.Service.State).
func (t *Trial) State(ctx context.Context) (jevtrial.State, error) { return t.svc.State(ctx) }

// Save validates and stores the form (see jevtrial.Service.Save) and records the
// keys it changed.
func (t *Trial) Save(ctx context.Context, f jevtrial.Form) error {
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("jevaudit: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	store := system.NewRuntimeSettingsRepositoryOn(tx)
	before, err := readTrialKeys(ctx, store)
	if err != nil {
		return err
	}
	if err := t.svc.WithSettings(store).Save(ctx, f); err != nil {
		return err
	}
	after, err := readTrialKeys(ctx, store)
	if err != nil {
		return err
	}
	// The keys a blank field removes: the form's own way to restore a default.
	reset := map[string]bool{
		jevtrialsettings.KeyDailyCostCapUSD:    f.Cap == nil,
		jevtrialsettings.KeyCodexDailyCallCap:  f.CodexCallCap == nil,
		jevtrialsettings.KeyCodexDailyTokenCap: f.CodexTokenCap == nil,
	}
	for _, key := range jevtrialsettings.OperatorKeys() {
		b, a := before[key], after[key]
		if reset[key] {
			if b.Overridden {
				if err := mcpset.RecordGUIChange(ctx, tx, t.now(), key, b, a, true); err != nil {
					return err
				}
			}
			continue
		}
		if b.Current != a.Current {
			if err := mcpset.RecordGUIChange(ctx, tx, t.now(), key, b, a, false); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("jevaudit: commit transaction: %w", err)
	}
	return nil
}

// readTrialKeys reads the six keys' effective values through store.
func readTrialKeys(ctx context.Context, store opsettings.Store) (map[string]opsettings.Value, error) {
	out := map[string]opsettings.Value{}
	for _, key := range jevtrialsettings.OperatorKeys() {
		v, err := trialkeys.Value(ctx, store, key)
		if err != nil {
			return nil, err
		}
		out[key] = v
	}
	return out, nil
}
