package sqlitedb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/ngrok/sqlmw"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// instrumentedDriverName is the database/sql driver name Open registers
// (once) and opens against: modernc.org/sqlite's "sqlite" driver wrapped
// with loggingInterceptor via github.com/ngrok/sqlmw, so every query/exec
// emits the DB latency/error structured log line non-functional.md §5.1
// requires. registerInstrumentedDriverOnce guards sql.Register, which
// panics on a duplicate name while Open may run many times per process.
const instrumentedDriverName = "sqlite-instrumented"

var registerInstrumentedDriverOnce sync.Once

const slowQueryThreshold = 100 * time.Millisecond

// registerInstrumentedDriver registers instrumentedDriverName the first
// time it is called; later calls are no-ops. modernc.org/sqlite's vec0
// module is process-global, so a locally constructed sqlite.Driver{} still
// sees it.
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
	recordDBWrite(err)
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
	recordDBWrite(err)
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
// back to a slower path), so it is not logged as an error. Failure is
// ERROR, a success taking >= slowQueryThreshold is WARN, any other success
// is DEBUG: idle scheduler workers poll every 200ms per queue, so INFO
// here bloats the daily log by hundreds of MB.
func logDBCall(query string, start time.Time, err error) {
	if err == driver.ErrSkip {
		return
	}
	elapsed := time.Since(start)
	attrs := []any{"query", query, "duration_ms", elapsed.Milliseconds()}
	switch {
	case err != nil:
		slog.Error("db: query failed", append(attrs, "error", err)...)
	case elapsed >= slowQueryThreshold:
		slog.Warn("db: slow query", attrs...)
	default:
		slog.Debug("db: query completed", attrs...)
	}
}

// DBWriteFailures is the process-wide consecutive DB write failure streak
// (FR-RISK-2 "DB書き込み失敗が一定回数継続") every Exec on an Open'd
// database feeds. It is process-global because the sqlmw-wrapped driver
// (registerInstrumentedDriver) is; internal/service/risk.Engine polls it
// via Config.DBWriteFailures.
var DBWriteFailures = &domain.FailureStreak{}

// recordDBWrite feeds one Exec outcome into DBWriteFailures. Only genuine
// storage failures (busy/locked/read-only/IO/full/cantopen/corrupt/notadb) count: a
// constraint violation or malformed statement is an application-level
// error, not evidence the database cannot be written, and a cancelled
// caller context is not a database fault at all.
func recordDBWrite(err error) {
	switch {
	case err == nil:
		DBWriteFailures.Succeed()
	case isStorageFailure(err):
		DBWriteFailures.Fail()
	}
}

func isStorageFailure(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	switch sqliteErr.Code() & 0xff { // primary result code
	case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED, sqlite3.SQLITE_READONLY,
		sqlite3.SQLITE_IOERR, sqlite3.SQLITE_FULL, sqlite3.SQLITE_CANTOPEN,
		sqlite3.SQLITE_CORRUPT, sqlite3.SQLITE_NOTADB:
		return true
	}
	return false
}
