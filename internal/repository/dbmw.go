package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"log/slog"
	"sync"
	"time"

	"github.com/ngrok/sqlmw"
	"modernc.org/sqlite"
)

// instrumentedDriverName is the database/sql driver name Open registers
// (once) and opens against: modernc.org/sqlite's own "sqlite" driver
// wrapped with loggingInterceptor via github.com/ngrok/sqlmw, so every
// query/exec this package's repositories issue emits the DB latency/error
// structured log line non-functional.md §5.1 requires ("DB latency /
// エラー") without every *_repo.go file needing its own timing code.
//
// A package-level sync.Once guards the sql.Register call: sql.Register
// panics if the same name is registered twice, and Open (this package's
// only caller of registerInstrumentedDriver) may run more than once per
// process (every repository_test.go helper calls repository.Open against
// its own t.TempDir() database).
const instrumentedDriverName = "sqlite-instrumented"

var registerInstrumentedDriverOnce sync.Once

// registerInstrumentedDriver registers instrumentedDriverName the first
// time it is called; later calls are no-ops. modernc.org/sqlite's vec0
// virtual table module (registered by this package's blank
// modernc.org/sqlite/vec import) is process-global, so a locally
// constructed sqlite.Driver{} still sees it (modernc.org/sqlite's own
// Driver doc comment: "Virtual table modules registered through the
// package-level path are held process-globally and reach every Driver").
func registerInstrumentedDriver() {
	registerInstrumentedDriverOnce.Do(func() {
		sql.Register(instrumentedDriverName, sqlmw.Driver(&sqlite.Driver{}, loggingInterceptor{}))
	})
}

// loggingInterceptor is a github.com/ngrok/sqlmw.Interceptor that logs
// every connection-level Exec/Query call's duration and error via slog
// (non-functional.md §5.1's structured-JSON "DB latency / エラー" line
// item). Embedding sqlmw.NullInterceptor makes every other Interceptor
// method a transparent passthrough.
type loggingInterceptor struct {
	sqlmw.NullInterceptor
}

func (loggingInterceptor) ConnExecContext(ctx context.Context, conn driver.ExecerContext, query string, args []driver.NamedValue) (driver.Result, error) {
	start := time.Now()
	res, err := conn.ExecContext(ctx, query, args)
	logDBCall(query, start, err)
	return res, err
}

func (loggingInterceptor) ConnQueryContext(ctx context.Context, conn driver.QueryerContext, query string, args []driver.NamedValue) (context.Context, driver.Rows, error) {
	start := time.Now()
	rows, err := conn.QueryContext(ctx, query, args)
	logDBCall(query, start, err)
	return ctx, rows, err
}

func (loggingInterceptor) StmtExecContext(ctx context.Context, stmt driver.StmtExecContext, query string, args []driver.NamedValue) (driver.Result, error) {
	start := time.Now()
	res, err := stmt.ExecContext(ctx, args)
	logDBCall(query, start, err)
	return res, err
}

func (loggingInterceptor) StmtQueryContext(ctx context.Context, stmt driver.StmtQueryContext, query string, args []driver.NamedValue) (context.Context, driver.Rows, error) {
	start := time.Now()
	rows, err := stmt.QueryContext(ctx, args)
	logDBCall(query, start, err)
	return ctx, rows, err
}

// logDBCall emits one structured JSON log line per DB call (query text,
// not args - args may carry business data that does not belong in logs).
// driver.ErrSkip is not a real failure (it tells database/sql to fall
// back to a slower path), so it is not logged as an error.
func logDBCall(query string, start time.Time, err error) {
	if err == driver.ErrSkip {
		return
	}
	attrs := []any{"query", query, "duration_ms", time.Since(start).Milliseconds()}
	if err != nil {
		slog.Error("db: query failed", append(attrs, "error", err)...)
		return
	}
	slog.Info("db: query completed", attrs...)
}
