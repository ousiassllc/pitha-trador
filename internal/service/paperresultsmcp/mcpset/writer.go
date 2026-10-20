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

// Writer is the MCP's only write path: one connection that runs exactly the
// two transactions of Set and SetEnabled.
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
		svc := newService(store, w.base)
		before, err := svc.Get(ctx, key)
		if err != nil {
			return err
		}
		if err := svc.Save(ctx, key, raw); err != nil {
			return err
		}
		after, err := svc.Get(ctx, key)
		if err != nil {
			return err
		}
		e := Entry{Actor: ActorMCP, Client: client, Action: ActionSet, Key: key, After: &after.Current}
		if before.Overridden {
			e.Before = &before.Current
		}
		out, err = insertAudit(ctx, tx, w.now(), e)
		return err
	})
	return out, err
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
