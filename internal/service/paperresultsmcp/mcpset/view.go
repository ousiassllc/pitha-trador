package mcpset

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Sources of a value.
const (
	// SourceStored: a runtime_settings row (the Settings screen or the MCP saved it).
	SourceStored = "stored"
	// SourceDefault: nothing is stored; the default (config/risk.yaml for the
	// Paper keys, the shipped value otherwise) applies.
	SourceDefault = "default"
)

// ErrUnknownSnapshot is returned by Diff for an audit id that does not exist.
var ErrUnknownSnapshot = errors.New("mcpset: unknown snapshot")

// Item is one whitelisted setting as settings_get shows it. Value and Default
// are the text the Settings screen shows; a secret-class key would carry
// mcpwritesettings.Masked.
type Item struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Unit        string `json:"unit"`
	Source      string `json:"source"`
	Default     string `json:"default"`
	Description string `json:"description"`
	Constraint  string `json:"constraint"`
	Warning     string `json:"warning,omitempty"`
}

// State is settings_get's result: every whitelisted key in whitelist order,
// the write switch and the snapshot id (the newest audit row; 0 when nothing
// was ever changed) to give settings_diff later.
type State struct {
	WriteEnabled bool   `json:"write_enabled"`
	SnapshotID   int64  `json:"snapshot_id"`
	Items        []Item `json:"items"`
}

// Current reads the effective value of every whitelisted key. base is
// config/risk.yaml's Paper section (see newService).
func Current(ctx context.Context, q query.Q, base *config.RiskLimits) (State, error) {
	store := ReadStore{Q: q}
	enabled, err := WriteEnabled(ctx, store)
	if err != nil {
		return State{}, err
	}
	snapshot, err := LatestAuditID(ctx, q)
	if err != nil {
		return State{}, err
	}
	svc := newService(store, base)
	st := State{WriteEnabled: enabled, SnapshotID: snapshot}
	for _, spec := range mcpwritesettings.Specs() {
		v, err := svc.Get(ctx, spec.Key)
		if err != nil {
			return State{}, fmt.Errorf("mcpset: read %s: %w", spec.Key, err)
		}
		it := Item{
			Key: spec.Key, Unit: spec.Unit, Source: SourceDefault, Description: spec.Description, Constraint: spec.Constraint,
			Value: mcpwritesettings.Mask(spec.Key, v.Current), Default: mcpwritesettings.Mask(spec.Key, v.Default),
		}
		if v.Overridden {
			it.Source = SourceStored
		}
		if !mcpwritesettings.IsSecret(spec.Key) {
			it.Warning = v.Warning
		}
		st.Items = append(st.Items, it)
	}
	return st, nil
}

// Snapshot selects the state settings_diff compares with. Both nil means
// "before the most recent save"; ID is an audit row id (the state right after
// that row; 0 is before everything); At is an instant (the state at that
// time). Set at most one.
type Snapshot struct {
	ID *int64
	At *time.Time
}

// Side is one end of a diff: the value and where it came from.
type Side struct {
	Value  string `json:"value"`
	Source string `json:"source"`
}

// Change is one key whose value differs between the snapshot and now.
type Change struct {
	Key       string `json:"key"`
	Unit      string `json:"unit"`
	Before    Side   `json:"before"`
	After     Side   `json:"after"`
	ChangedAt string `json:"changed_at"`
	ChangedBy string `json:"changed_by"`
}

// Since says which snapshot a Diff compared with: the audit row id the state
// is taken after (0: before every row) and, for a row, its time.
type Since struct {
	ID int64  `json:"id"`
	At string `json:"at,omitempty"`
}

// Diff is settings_diff's result. Changes lists only keys whose value or
// source differs, in whitelist order; SnapshotID is the newest audit row now.
type Diff struct {
	Since      Since    `json:"since"`
	SnapshotID int64    `json:"snapshot_id"`
	Changes    []Change `json:"changes"`
}

// DiffSince compares the current whitelisted values with the snapshot. The
// state at a snapshot is rebuilt from the audit log: a key with no audit row
// after the snapshot is taken as unchanged since then; for a key with rows,
// its value before the first of them is the value at the snapshot. A change
// made outside the audited paths (directly in the database) is therefore not
// seen unless the key was also changed through an audited path.
func DiffSince(ctx context.Context, q query.Q, base *config.RiskLimits, snap Snapshot) (Diff, error) {
	cur, err := Current(ctx, q, base)
	if err != nil {
		return Diff{}, err
	}
	since, err := resolveSince(ctx, q, snap, cur.SnapshotID)
	if err != nil {
		return Diff{}, err
	}
	rows, err := q.QueryContext(ctx, `SELECT `+auditColumns+` FROM mcp_settings_audit WHERE id > ? AND action IN (?, ?) ORDER BY id`,
		since.ID, ActionSet, ActionReset)
	if err != nil {
		return Diff{}, fmt.Errorf("mcpset: read audit rows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	first, last := map[string]Entry{}, map[string]Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return Diff{}, fmt.Errorf("mcpset: scan audit row: %w", err)
		}
		if _, ok := first[e.Key]; !ok {
			first[e.Key] = e
		}
		last[e.Key] = e
	}
	if err := rows.Err(); err != nil {
		return Diff{}, fmt.Errorf("mcpset: read audit rows: %w", err)
	}

	d := Diff{Since: since, SnapshotID: cur.SnapshotID, Changes: []Change{}}
	for _, it := range cur.Items {
		f, ok := first[it.Key]
		if !ok {
			continue
		}
		before := Side{Value: it.Default, Source: SourceDefault}
		if f.Before != nil {
			before = Side{Value: *f.Before, Source: SourceStored}
		}
		after := Side{Value: it.Value, Source: it.Source}
		if before == after {
			continue
		}
		l := last[it.Key]
		by := l.Actor
		if l.Client != "" {
			by += ":" + l.Client
		}
		d.Changes = append(d.Changes, Change{Key: it.Key, Unit: it.Unit, Before: before, After: after, ChangedAt: l.At, ChangedBy: by})
	}
	return d, nil
}

func resolveSince(ctx context.Context, q query.Q, snap Snapshot, latest int64) (Since, error) {
	switch {
	case snap.ID != nil:
		id := *snap.ID
		if id < 0 || id > latest {
			return Since{}, ErrUnknownSnapshot
		}
		return sinceRow(ctx, q, id)
	case snap.At != nil:
		var id sql.NullInt64
		if err := q.QueryRowContext(ctx, `SELECT MAX(id) FROM mcp_settings_audit WHERE at <= ?`, sqlutil.FormatTime(*snap.At)).Scan(&id); err != nil {
			return Since{}, fmt.Errorf("mcpset: resolve snapshot time: %w", err)
		}
		return sinceRow(ctx, q, id.Int64)
	}
	var id sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT MAX(id) FROM mcp_settings_audit WHERE action IN (?, ?)`, ActionSet, ActionReset).Scan(&id); err != nil {
		return Since{}, fmt.Errorf("mcpset: resolve last save: %w", err)
	}
	if id.Int64 == 0 {
		return Since{}, nil
	}
	since, err := sinceRow(ctx, q, id.Int64-1)
	if errors.Is(err, ErrUnknownSnapshot) {
		return Since{ID: id.Int64 - 1}, nil // the row before was pruned: its id still bounds the diff
	}
	return since, err
}

// sinceRow returns Since for audit row id (0: before everything).
func sinceRow(ctx context.Context, q query.Q, id int64) (Since, error) {
	s := Since{ID: id}
	if id == 0 {
		return s, nil
	}
	var at string
	err := q.QueryRowContext(ctx, `SELECT at FROM mcp_settings_audit WHERE id = ?`, id).Scan(&at)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Since{}, ErrUnknownSnapshot
	case err != nil:
		return Since{}, fmt.Errorf("mcpset: read snapshot row: %w", err)
	}
	if t, perr := sqlutil.ParseTime(at); perr == nil {
		s.At = t.Format(time.RFC3339)
	}
	return s, nil
}
