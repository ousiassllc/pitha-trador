package paperresultsmcp

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"modernc.org/sqlite"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/readdb"
)

// Code is an error code of docs/architecture/paper-results-mcp-errors.md §1.
// not_listening has no value here: it is the absence of a listener.
type Code string

// The codes. The HTTP status and the fixed message of each are in codeTable.
const (
	CodeUnauthorized       Code = "unauthorized"
	CodeAccessDenied       Code = "access_denied"
	CodeInvalidPeriod      Code = "invalid_period"
	CodeInvalidArgument    Code = "invalid_argument"
	CodeLimitExceeded      Code = "limit_exceeded"
	CodeDBLocked           Code = "db_locked"
	CodeDBUnavailable      Code = "db_unavailable"
	CodeQueryTimeout       Code = "query_timeout"
	CodeConcurrencyLimited Code = "concurrency_limited"
	CodeInternal           Code = "internal"
)

type codeInfo struct {
	status  int
	message string
}

// codeTable is the errors document's table. The message is the only text a
// client ever sees for a failure: no SQL, path, header value or stack.
var codeTable = map[Code]codeInfo{
	CodeUnauthorized:       {http.StatusUnauthorized, "認証に失敗しました。"},
	CodeAccessDenied:       {http.StatusUnauthorized, "認証に失敗しました。"},
	CodeInvalidPeriod:      {http.StatusUnprocessableEntity, "期間の指定が不正です。"},
	CodeInvalidArgument:    {http.StatusUnprocessableEntity, "引数が不正です。"},
	CodeLimitExceeded:      {http.StatusUnprocessableEntity, "取得量が上限を超えています。期間を狭めてください。"},
	CodeDBLocked:           {http.StatusServiceUnavailable, "データベースが使用中です。しばらくしてから再度呼んでください。"},
	CodeDBUnavailable:      {http.StatusServiceUnavailable, "データベースを読み取れません。"},
	CodeQueryTimeout:       {http.StatusGatewayTimeout, "読み取りが時間内に終わりませんでした。"},
	CodeConcurrencyLimited: {http.StatusTooManyRequests, "同時に処理できる件数を超えています。"},
	CodeInternal:           {http.StatusInternalServerError, "読み取りに失敗しました。"},
}

// toolError is a failure with a spec code. cause is for the server log only.
type toolError struct {
	code  Code
	cause error
}

func (e *toolError) Error() string {
	if e.cause != nil {
		return string(e.code) + ": " + e.cause.Error()
	}
	return string(e.code)
}

func (e *toolError) Unwrap() error { return e.cause }

func fail(code Code) *toolError { return &toolError{code: code} }

// classify maps any error from a tool run to its spec code. ctx is the
// per-call context: its deadline decides query_timeout even when the driver
// reports the interruption as a generic error.
func classify(ctx context.Context, err error) *toolError {
	var te *toolError
	switch {
	case errors.As(err, &te):
		return te
	case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		return &toolError{CodeQueryTimeout, err}
	case errors.Is(err, query.ErrLimitExceeded):
		return &toolError{CodeLimitExceeded, err}
	case errors.Is(err, query.ErrBadCursor):
		return &toolError{CodeInvalidArgument, err}
	case errors.Is(err, readdb.ErrUnavailable), errors.Is(err, sql.ErrConnDone):
		return &toolError{CodeDBUnavailable, err}
	case isBusy(err):
		return &toolError{CodeDBLocked, err}
	case isMissingSchema(err):
		return &toolError{CodeDBUnavailable, err}
	}
	return &toolError{CodeInternal, err}
}

// SQLite primary result codes (sqlite3.h).
const (
	sqliteBusy   = 5
	sqliteLocked = 6
)

func isBusy(err error) bool {
	if strings.Contains(err.Error(), "database is locked") {
		return true
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		primary := se.Code() & 0xff
		return primary == sqliteBusy || primary == sqliteLocked
	}
	return false
}

// isMissingSchema reports a table or column the tool needs that the file
// lacks (an older schema).
func isMissingSchema(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "no such table") || strings.Contains(msg, "no such column")
}
