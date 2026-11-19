// Package unrecorded finds changes of the tuned variables - a whitelisted
// setting, the Jev model, a Jev prompt version - that no tuning experiment
// records (issue #1088). It is the read-only notice experiments_list returns
// next to the experiments, so that a change made without an experiment_create
// (or a settings_set with a hypothesis) is visible instead of silently
// confounding later comparisons. It writes nothing and never creates an
// experiment, changes a setting or reverts anything.
package unrecorded

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Limits of the notice.
const (
	// WindowDays is how far back a change is looked for.
	WindowDays = 14
	// MaxChanges is the most changes returned, newest first.
	MaxChanges = 20
	// JevCoverSlack is how long before a model / prompt change an experiment's
	// started_at may lie and still record it: a model switch is saved some time
	// before the application restarts on it (the experiment starts when the
	// change takes effect, not when it was saved), a prompt version is seen in
	// decisions only after it was deployed.
	JevCoverSlack = 24 * time.Hour
	// maxScanRows bounds the audit rows read (a window of 14 days never holds
	// more in practice).
	maxScanRows = 500
)

// Change is one change no experiment records.
type Change struct {
	// Kind is setting, model or prompt (the kinds of an experiment's variable).
	Kind string `json:"kind"`
	// Name is the setting key, experiment.ModelSwitchName for a model switch, or
	// the Jev decision type (scout / trader) whose prompt version changed.
	Name string `json:"name"`
	// ChangedAt is when it was saved (a setting, a model) or first used (a prompt version), UTC RFC3339.
	ChangedAt string `json:"changed_at"`
	// AuditID is the mcp_settings_audit row of a setting change.
	AuditID *int64 `json:"audit_id,omitempty"`
	// Before / After are the values (secret keys are masked); null: not set / unknown.
	Before *string `json:"before"`
	After  *string `json:"after"`
}

// recorded is the part of a tuning_experiments row that decides whether it covers a change.
type recorded struct {
	kind, name, after string
	audit             *int64
	started           time.Time
}

// List returns the uncovered changes of the last WindowDays days at now, newest
// first, at most MaxChanges.
func List(ctx context.Context, q query.Q, now time.Time) ([]Change, error) {
	since := now.AddDate(0, 0, -WindowDays)
	exps, err := experiments(ctx, q, since.Add(-JevCoverSlack))
	if err != nil {
		return nil, err
	}
	out := []Change{}
	settings, err := settingChanges(ctx, q, since, exps)
	if err != nil {
		return nil, err
	}
	out = append(out, settings...)
	models, err := modelChanges(ctx, q, since, exps)
	if err != nil {
		return nil, err
	}
	out = append(out, models...)
	prompts, err := promptChanges(ctx, q, since, exps)
	if err != nil {
		return nil, err
	}
	out = append(out, prompts...)
	slices.SortStableFunc(out, func(a, b Change) int {
		return cmp.Or(strings.Compare(b.ChangedAt, a.ChangedAt), strings.Compare(a.Kind+a.Name, b.Kind+b.Name))
	})
	return out[:min(len(out), MaxChanges)], nil
}

// experiments reads the experiments started since (any status: an aborted one
// still recorded the change).
func experiments(ctx context.Context, q query.Q, since time.Time) ([]recorded, error) {
	rows, err := q.QueryContext(ctx, `SELECT variable_kind, variable_name, after_value, audit_id, started_at FROM tuning_experiments WHERE started_at >= ?`, sqlutil.FormatTime(since))
	if err != nil {
		return nil, fmt.Errorf("unrecorded: read experiments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []recorded
	for rows.Next() {
		var (
			r       recorded
			audit   sql.NullInt64
			started string
		)
		if err := rows.Scan(&r.kind, &r.name, &r.after, &audit, &started); err != nil {
			return nil, fmt.Errorf("unrecorded: scan experiment: %w", err)
		}
		if audit.Valid {
			r.audit = &audit.Int64
		}
		if r.started, err = sqlutil.ParseTime(started); err != nil {
			return nil, fmt.Errorf("unrecorded: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// settingChanges lists the whitelisted settings changed since (mcp_settings_audit,
// actor mcp or gui; a save that left the value as it was is no change) that no
// experiment covers: one with that audit_id, or one of the same setting whose
// start lies within experiment.StartTolerance of the save (the rule of the
// switch check).
func settingChanges(ctx context.Context, q query.Q, since time.Time, exps []recorded) ([]Change, error) {
	rows, err := q.QueryContext(ctx, `
SELECT id, at, setting_key, before_value, after_value FROM mcp_settings_audit
WHERE at >= ? AND action IN ('set', 'reset') AND actor IN ('mcp', 'gui') ORDER BY id DESC LIMIT ?`, sqlutil.FormatTime(since), maxScanRows)
	if err != nil {
		return nil, fmt.Errorf("unrecorded: read setting changes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Change
	for rows.Next() {
		var (
			id       int64
			at, key  string
			bef, aft *string
		)
		if err := rows.Scan(&id, &at, &key, &bef, &aft); err != nil {
			return nil, fmt.Errorf("unrecorded: scan setting change: %w", err)
		}
		saved, err := sqlutil.ParseTime(at)
		if err != nil {
			return nil, fmt.Errorf("unrecorded: %w", err)
		}
		if !mcpwritesettings.IsAllowed(key) || unchanged(bef, aft) || coveredSetting(exps, id, key, saved) {
			continue
		}
		out = append(out, Change{Kind: experiment.KindSetting, Name: key, ChangedAt: saved.Format(time.RFC3339), AuditID: &id, Before: masked(key, bef), After: masked(key, aft)})
	}
	return out, rows.Err()
}

func coveredSetting(exps []recorded, auditID int64, key string, saved time.Time) bool {
	return slices.ContainsFunc(exps, func(e recorded) bool {
		if e.kind != experiment.KindSetting {
			return false
		}
		return (e.audit != nil && *e.audit == auditID) || (e.name == key && e.started.Sub(saved).Abs() <= experiment.StartTolerance)
	})
}

// modelChanges lists the Jev model switches saved since (jev_model_changes, the
// Settings screen's record) with no model experiment to the same model.
func modelChanges(ctx context.Context, q query.Q, since time.Time, exps []recorded) ([]Change, error) {
	rows, err := q.QueryContext(ctx, `SELECT at, before_value, after_value FROM jev_model_changes WHERE at >= ? ORDER BY id DESC LIMIT ?`, sqlutil.FormatTime(since), maxScanRows)
	if err != nil {
		return nil, fmt.Errorf("unrecorded: read model changes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Change
	for rows.Next() {
		var (
			at       string
			bef, aft *string
		)
		if err := rows.Scan(&at, &bef, &aft); err != nil {
			return nil, fmt.Errorf("unrecorded: scan model change: %w", err)
		}
		saved, err := sqlutil.ParseTime(at)
		if err != nil {
			return nil, fmt.Errorf("unrecorded: %w", err)
		}
		if !coveredJev(exps, experiment.KindModel, aft, saved) {
			out = append(out, Change{Kind: experiment.KindModel, Name: experiment.ModelSwitchName, ChangedAt: saved.Format(time.RFC3339), Before: bef, After: aft})
		}
	}
	return out, rows.Err()
}

// coveredJev: a model / prompt experiment whose after is the new value and that
// started no earlier than JevCoverSlack before the change.
func coveredJev(exps []recorded, kind string, after *string, changed time.Time) bool {
	return after != nil && slices.ContainsFunc(exps, func(e recorded) bool {
		return e.kind == kind && e.after == *after && !e.started.Before(changed.Add(-JevCoverSlack))
	})
}

// promptChanges lists the Jev prompt versions (question_version) that first
// appeared in decisions since, per decision type, after another version was in
// use (the one in use before the window, else the first one seen in it), with
// no prompt experiment to that version.
func promptChanges(ctx context.Context, q query.Q, since time.Time, exps []recorded) ([]Change, error) {
	type first struct {
		version string
		at      time.Time
	}
	rows, err := q.QueryContext(ctx, `
SELECT decision_type, question_version, MIN(timestamp) FROM jev_decisions WHERE timestamp >= ? GROUP BY decision_type, question_version`, sqlutil.FormatTime(since))
	if err != nil {
		return nil, fmt.Errorf("unrecorded: read prompt versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	seen := map[string][]first{}
	for rows.Next() {
		var typ, version, at string
		if err := rows.Scan(&typ, &version, &at); err != nil {
			return nil, fmt.Errorf("unrecorded: scan prompt version: %w", err)
		}
		t, err := sqlutil.ParseTime(at)
		if err != nil {
			return nil, fmt.Errorf("unrecorded: %w", err)
		}
		seen[typ] = append(seen[typ], first{version, t})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("unrecorded: read prompt versions: %w", err)
	}
	var out []Change
	for _, typ := range slices.Sorted(maps.Keys(seen)) {
		versions := seen[typ]
		slices.SortFunc(versions, func(a, b first) int { return a.at.Compare(b.at) })
		var prev sql.NullString
		err := q.QueryRowContext(ctx, `SELECT question_version FROM jev_decisions WHERE decision_type = ? AND timestamp < ? ORDER BY timestamp DESC LIMIT 1`,
			typ, sqlutil.FormatTime(since)).Scan(&prev)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("unrecorded: read the prompt version before the window: %w", err)
		}
		known := prev.String
		for i, v := range versions {
			switch {
			case v.version == known:
			case known == "" && i == 0: // the version the window opens on
				known = v.version
			default:
				change := Change{Kind: experiment.KindPrompt, Name: typ, ChangedAt: v.at.Format(time.RFC3339), After: new(v.version)}
				if known != "" {
					change.Before = new(known)
				}
				if !coveredJev(exps, experiment.KindPrompt, change.After, v.at) {
					out = append(out, change)
				}
				known = v.version
			}
		}
	}
	return out, nil
}

// masked hides the value of a secret-class key (the audit table masks it
// already; this keeps the guarantee local).
func masked(key string, v *string) *string {
	if v == nil || !mcpwritesettings.IsSecret(key) {
		return v
	}
	return new(mcpwritesettings.Masked)
}

// unchanged reports whether an audit row left the value as it was (the same
// text or number stored again, or a reset of a key with no stored value).
func unchanged(before, after *string) bool {
	if before == nil || after == nil {
		return before == nil && after == nil
	}
	a, b := strings.TrimSpace(*before), strings.TrimSpace(*after)
	if a == b {
		return true
	}
	fa, ea := strconv.ParseFloat(a, 64)
	fb, eb := strconv.ParseFloat(b, 64)
	return ea == nil && eb == nil && fa == fb
}
