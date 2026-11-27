package proposalexectests

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"modernc.org/sqlite"

	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/repository/proposalstore"
)

// panicOnceConnector opens the database at dsn; the first query on
// order_proposals (the processor's Active) panics, every later one runs.
type panicOnceConnector struct {
	dsn   string
	fired atomic.Bool
}

func (c *panicOnceConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := (&sqlite.Driver{}).Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return panicOnceConn{Conn: conn, c: c}, nil
}

func (c *panicOnceConnector) Driver() driver.Driver { return &sqlite.Driver{} }

type panicOnceConn struct {
	driver.Conn
	c *panicOnceConnector
}

func (p panicOnceConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "FROM order_proposals") && p.c.fired.CompareAndSwap(false, true) {
		panic("proposal store bug")
	}
	return p.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
}

// A panic in the cycle itself (outside the per-proposal guard), here in
// Proposals.Active, is logged and the loop carries on: the next tick still
// processes the proposal (FR-SCHED-6).
func TestRun_ResumesAfterAPanicInTheCycle(t *testing.T) {
	f := newFixture(t)
	var path string
	if err := f.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	connector := &panicOnceConnector{dsn: path}
	panicDB := sql.OpenDB(connector)
	t.Cleanup(func() { _ = panicDB.Close() })
	f.proc.Proposals = proposalstore.New(panicDB, func() time.Time { return *f.clock })
	id := f.propose(spec{expires: at(10, 0, 0)}) // already expired at the fixture's clock

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); f.proc.Run(ctx, 5*time.Millisecond) }()
	t.Cleanup(func() { cancel(); <-done })

	deadline := time.After(10 * time.Second)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for f.get(id).Status != orderproposal.StatusExpired {
		select {
		case <-deadline:
			t.Fatalf("proposal still %q after the cycle panicked (panic fired: %v); the loop did not resume", f.get(id).Status, connector.fired.Load())
		case <-tick.C:
		}
	}
	if !connector.fired.Load() {
		t.Fatal("the panic was never triggered")
	}
}
