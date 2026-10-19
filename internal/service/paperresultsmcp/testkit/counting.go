package testkit

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// CountingDriver wraps the real "sqlite" driver and records every connection
// opened and every statement prepared through it. It proves, without
// trusting the handler's own bookkeeping, that a request reached no SQL and
// opened no database connection.
type CountingDriver struct {
	inner driver.Driver

	Opens      atomic.Int64
	Statements atomic.Int64

	mu    sync.Mutex
	log   []string
	onSQL func(ctx context.Context, query string) error
}

// NewCountingDriver registers a uniquely named wrapper of the sqlite driver.
func NewCountingDriver(t *testing.T) (name string, d *CountingDriver) {
	t.Helper()
	probe, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	inner := probe.Driver()
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	d = &CountingDriver{inner: inner}
	name = "counting-" + strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	sql.Register(name, d)
	return name, d
}

func (d *CountingDriver) Open(dsn string) (driver.Conn, error) {
	d.Opens.Add(1)
	c, err := d.inner.Open(dsn)
	if err != nil {
		return nil, err
	}
	return &countingConn{Conn: c, d: d}, nil
}

// StatementLog returns the SQL texts prepared so far.
func (d *CountingDriver) StatementLog() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.log...)
}

// SetOnSQL installs a hook run before each statement (nil removes it).
func (d *CountingDriver) SetOnSQL(f func(ctx context.Context, query string) error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.onSQL = f
}

func (d *CountingDriver) record(ctx context.Context, query string) error {
	d.Statements.Add(1)
	d.mu.Lock()
	d.log = append(d.log, query)
	hook := d.onSQL
	d.mu.Unlock()
	if hook != nil {
		return hook(ctx, query)
	}
	return nil
}

type countingConn struct {
	driver.Conn
	d *CountingDriver
}

func (c *countingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if err := c.d.record(ctx, query); err != nil {
		return nil, err
	}
	if p, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return p.PrepareContext(ctx, query)
	}
	return c.Prepare(query)
}

func (c *countingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	// A transaction is SQL (BEGIN) to SQLite, so it counts as a statement.
	if err := c.d.record(ctx, "BEGIN"); err != nil {
		return nil, err
	}
	if b, ok := c.Conn.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, opts)
	}
	return c.Conn.Begin() //nolint:staticcheck // fallback for drivers without ConnBeginTx
}
