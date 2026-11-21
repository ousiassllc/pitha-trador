package mcpset

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

var (
	// ErrKeyNotAllowed is returned for a key that is not on the whitelist
	// (mcpwritesettings.Keys). Nothing was read or stored.
	ErrKeyNotAllowed = errors.New("mcpset: key is not writable from the MCP")
	// ErrWriteDisabled is returned while the write switch is off. Nothing was stored.
	ErrWriteDisabled = errors.New("mcpset: settings writes are switched off")
	// ErrUnavailable wraps every failure to open the write connection.
	ErrUnavailable = errors.New("mcpset: write connection unavailable")
)

// WriterBusyTimeout is how long a write waits for the application's lock.
const WriterBusyTimeout = 2 * time.Second

// writerDSNQuery: mode=rw never creates the file; _txlock=immediate takes the
// write lock at BEGIN, so a busy database fails fast instead of upgrading
// mid-transaction. journal_mode is not touched (the application owns WAL).
const writerDSNQuery = "mode=rw&_busy_timeout=2000&_txlock=immediate"

// Writer is the MCP's only write path: one connection whose every method runs
// one narrow transaction (the package comment in audit.go lists them).
type Writer struct {
	db   *sql.DB
	base *config.RiskLimits
	now  func() time.Time
}

// OpenWriter opens the existing database file at path for writing with the
// registered driverName. It never creates the file and runs no migration: the
// application owns the schema. base is config/risk.yaml's Paper section (may be nil).
func OpenWriter(driverName, path string, base *config.RiskLimits) (*Writer, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%w: path is a directory", ErrUnavailable)
	}
	db, err := sql.Open(driverName, sqlitedb.FileURI(path, writerDSNQuery))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	db.SetMaxOpenConns(1)
	return &Writer{db: db, base: base, now: time.Now}, nil
}

// SetClock replaces the clock; tests use it.
func (w *Writer) SetClock(now func() time.Time) { w.now = now }

// Close releases the connection.
func (w *Writer) Close() error { return w.db.Close() }

// Set validates raw for key like the Settings screen does and stores it, in
// one transaction with its audit row. The checks run in this order, each
// before anything is stored: the whitelist (ErrKeyNotAllowed), the write
// switch (ErrWriteDisabled), the value (*opsettings.InvalidValueError or
// opsettings.ErrEmptyValue). client is the caller's host (audit "who").
func (w *Writer) Set(ctx context.Context, client, key, raw string) (Entry, error) {
	if !mcpwritesettings.IsAllowed(key) {
		return Entry{}, ErrKeyNotAllowed
	}
	var out Entry
	err := w.inTx(ctx, func(store *system.RuntimeSettingsRepository, tx sqlExecer) error {
		on, err := WriteEnabled(ctx, store)
		if err != nil {
			return err
		}
		if !on {
			return ErrWriteDisabled
		}
		out, _, _, err = w.saveAndAudit(ctx, store, tx, client, key, raw)
		return err
	})
	return out, err
}

// saveAndAudit is the write of Set inside its transaction (the write switch has
// been checked): validate and store raw for key, then append the audit row. It
// also returns the key's effective value before and after, which the
// experiment-recording variant of Set (setexperiment.go) stores as the
// experiment's before / after.
func (w *Writer) saveAndAudit(ctx context.Context, store *system.RuntimeSettingsRepository, tx sqlExecer, client, key, raw string) (entry Entry, before, after opsettings.Value, err error) {
	svc := newService(store, w.base)
	if before, err = getSetting(ctx, store, svc, key); err != nil {
		return Entry{}, before, after, err
	}
	if err = saveSetting(ctx, store, svc, key, raw, w.now()); err != nil {
		return Entry{}, before, after, err
	}
	if after, err = getSetting(ctx, store, svc, key); err != nil {
		return Entry{}, before, after, err
	}
	e := Entry{Actor: ActorMCP, Client: client, Action: ActionSet, Key: key, After: &after.Current}
	if before.Overridden {
		e.Before = &before.Current
	}
	entry, err = insertAudit(ctx, tx, w.now(), e)
	return entry, before, after, err
}

// SetEnabled turns the write switch on or off and records it. It works while
// writes are off (it is how they are turned back on) and is the MCP twin of
// the Settings screen's toggle.
func (w *Writer) SetEnabled(ctx context.Context, client string, enabled bool) (Entry, error) {
	var out Entry
	err := w.inTx(ctx, func(store *system.RuntimeSettingsRepository, tx sqlExecer) error {
		was, err := WriteEnabled(ctx, store)
		if err != nil {
			return err
		}
		if err := store.Set(ctx, mcpwritesettings.KeyWriteEnabled, mcpwritesettings.Text(enabled), w.now()); err != nil {
			return err
		}
		out, err = insertAudit(ctx, tx, w.now(), switchEntry(ActorMCP, client, was, enabled))
		return err
	})
	return out, err
}

// sqlExecer is the transaction handle the closures get.
type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (w *Writer) inTx(ctx context.Context, fn func(*system.RuntimeSettingsRepository, sqlExecer) error) error {
	return runInTx(ctx, w.db, "write transaction", func(tx *sql.Tx) error {
		return fn(system.NewRuntimeSettingsRepositoryOn(tx), tx)
	})
}

// InTx runs fn in one write transaction on the writer's connection, with the
// writer's clock. When gated, the write switch is read first inside the same
// transaction and ErrWriteDisabled is returned (nothing written) while it is
// off. It is the one transaction runner of mcpset/researchwrite, the research
// tables' single-statement writes; what names the transaction in its errors.
func (w *Writer) InTx(ctx context.Context, what string, gated bool, fn func(tx *sql.Tx, now time.Time) error) error {
	return runInTx(ctx, w.db, what, func(tx *sql.Tx) error {
		if gated {
			on, err := WriteEnabled(ctx, system.NewRuntimeSettingsRepositoryOn(tx))
			if err != nil {
				return err
			}
			if !on {
				return ErrWriteDisabled
			}
		}
		return fn(tx, w.now())
	})
}

// switchEntry is the audit row of a write switch change.
func switchEntry(actor, client string, was, now bool) Entry {
	action := ActionWriteDisable
	if now {
		action = ActionWriteEnable
	}
	return Entry{
		Actor: actor, Client: client, Action: action, Key: mcpwritesettings.KeyWriteEnabled,
		Before: new(mcpwritesettings.Text(was)), After: new(mcpwritesettings.Text(now)),
	}
}

// IsInvalidValue reports whether err is a value the validators rejected, and
// returns the operator-facing reason (it quotes only the value the caller
// sent and the accepted range; nothing whitelisted is secret).
func IsInvalidValue(err error) (reason string, ok bool) {
	var iv *opsettings.InvalidValueError
	switch {
	case errors.As(err, &iv):
		return strings.ReplaceAll(iv.Reason, "\n", "; "), true
	case errors.Is(err, opsettings.ErrEmptyValue):
		return "値が空です", true
	}
	return "", false
}
