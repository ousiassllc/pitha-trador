package paperresultsmcp

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"modernc.org/sqlite"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
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
	// The settings tools' codes (issues #806, #807).
	CodeWriteDisabled Code = "write_disabled"
	CodeKeyNotAllowed Code = "key_not_allowed"
	CodeInvalidValue  Code = "invalid_value"
	// The order-proposal tools' codes (issue #827).
	CodeProposalsDisabled   Code = "proposals_disabled"
	CodeIdempotencyConflict Code = "idempotency_conflict"
	CodeProposalNotFound    Code = "proposal_not_found"
	CodeNotCancellable      Code = "not_cancellable"
	// The tuning-experiment tools' codes (issue #871).
	CodeMultipleVariables    Code = "multiple_variables"
	CodeExperimentNotFound   Code = "experiment_not_found"
	CodeExperimentNotRunning Code = "experiment_not_running"
)

type codeInfo struct {
	status  int
	message string
}

// codeTable is the errors document's table. The message is the only text a
// client ever sees for a failure: no SQL, path, header value or stack.
var codeTable = map[Code]codeInfo{
	CodeUnauthorized:         {http.StatusUnauthorized, "認証に失敗しました。"},
	CodeAccessDenied:         {http.StatusUnauthorized, "認証に失敗しました。"},
	CodeInvalidPeriod:        {http.StatusUnprocessableEntity, "期間の指定が不正です。"},
	CodeInvalidArgument:      {http.StatusUnprocessableEntity, "引数が不正です。"},
	CodeLimitExceeded:        {http.StatusUnprocessableEntity, "取得量が上限を超えています。期間を狭めてください。"},
	CodeDBLocked:             {http.StatusServiceUnavailable, "データベースが使用中です。しばらくしてから再度呼んでください。"},
	CodeDBUnavailable:        {http.StatusServiceUnavailable, "データベースを読み取れません。"},
	CodeQueryTimeout:         {http.StatusGatewayTimeout, "読み取りが時間内に終わりませんでした。"},
	CodeConcurrencyLimited:   {http.StatusTooManyRequests, "同時に処理できる件数を超えています。"},
	CodeInternal:             {http.StatusInternalServerError, "読み取りに失敗しました。"},
	CodeWriteDisabled:        {http.StatusForbidden, "設定の書き込みは許可されていません。settings_write_enable か Settings の「外部連携」で許可してください。"},
	CodeKeyNotAllowed:        {http.StatusForbidden, "このキーは MCP から変更できません。settings_get に出るキーだけ変更できます。"},
	CodeInvalidValue:         {http.StatusUnprocessableEntity, "値が不正です。settings_get の constraint を確認してください。"},
	CodeProposalsDisabled:    {http.StatusForbidden, "注文案の受け付けはオフです。Settings の「外部連携」でオンにしてください（MCP からはオンにできません）。"},
	CodeIdempotencyConflict:  {http.StatusConflict, "この idempotency_key は内容の違う案で使われています。別のキーで出し直してください。"},
	CodeProposalNotFound:     {http.StatusNotFound, "その proposal_id の注文案はありません。"},
	CodeNotCancellable:       {http.StatusConflict, "この注文案はもう取り消せません（約定済み・終了済み、または処理中）。order_proposal_get で状態を確認してください。"},
	CodeMultipleVariables:    {http.StatusUnprocessableEntity, "1 つの実験で変えられる変数は 1 つだけです。changes を 1 つにして、変数ごとに実験を分けてください。"},
	CodeExperimentNotFound:   {http.StatusNotFound, "その experiment_id の実験はありません。experiments_list で確認してください。"},
	CodeExperimentNotRunning: {http.StatusConflict, "この実験はもう終わっています（再び開くことはできません）。"},
}

// toolError is a failure with a spec code. cause is for the server log only.
type toolError struct {
	code  Code
	cause error
	// detail is shown to the client in error.data.detail: invalid_value's
	// validator reason (it quotes the caller's own value and the accepted range;
	// no whitelisted setting is secret), and the order-proposal tools'
	// invalid_argument / limit_exceeded reasons (they name the argument and the
	// rule, never a value).
	detail string
}

func (e *toolError) Error() string {
	if e.cause != nil {
		return string(e.code) + ": " + e.cause.Error()
	}
	return string(e.code)
}

func (e *toolError) Unwrap() error { return e.cause }

func fail(code Code) *toolError { return &toolError{code: code} }

// failDetail is fail with a client-visible detail (see toolError.detail).
func failDetail(code Code, detail string) *toolError { return &toolError{code: code, detail: detail} }

// classify maps any error from a tool run to its spec code. ctx is the
// per-call context: its deadline decides query_timeout even when the driver
// reports the interruption as a generic error.
func classify(ctx context.Context, err error) *toolError {
	var te *toolError
	switch {
	case errors.As(err, &te):
		return te
	case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		return &toolError{code: CodeQueryTimeout, cause: err}
	case errors.Is(err, query.ErrLimitExceeded):
		return &toolError{code: CodeLimitExceeded, cause: err}
	case errors.Is(err, query.ErrBadCursor):
		return &toolError{code: CodeInvalidArgument, cause: err}
	case errors.Is(err, readdb.ErrUnavailable), errors.Is(err, mcpset.ErrUnavailable), errors.Is(err, sql.ErrConnDone):
		return &toolError{code: CodeDBUnavailable, cause: err}
	case isBusy(err):
		return &toolError{code: CodeDBLocked, cause: err}
	case isMissingSchema(err):
		return &toolError{code: CodeDBUnavailable, cause: err}
	}
	return &toolError{code: CodeInternal, cause: err}
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
