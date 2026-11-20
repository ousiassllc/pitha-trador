package tooldef

// The spec error codes a tool package may raise: the string values of the
// root's Code constants (docs/architecture/paper-results-mcp-errors.md §1).
// The root's tests check that each exists in its code table.
const (
	CodeInvalidArgument     = "invalid_argument"
	CodeLimitExceeded       = "limit_exceeded"
	CodeConcurrencyLimited  = "concurrency_limited"
	CodeWriteDisabled       = "write_disabled"
	CodeInvalidValue        = "invalid_value"
	CodeBacktestNotFound    = "backtest_not_found"
	CodeBacktestUnavailable = "backtest_unavailable"

	CodeKeyNotAllowed        = "key_not_allowed"
	CodeProposalsDisabled    = "proposals_disabled"
	CodeIdempotencyConflict  = "idempotency_conflict"
	CodeProposalNotFound     = "proposal_not_found"
	CodeNotCancellable       = "not_cancellable"
	CodeMultipleVariables    = "multiple_variables"
	CodeExperimentNotFound   = "experiment_not_found"
	CodeExperimentNotRunning = "experiment_not_running"
)

// Failure is a failure with a spec code. Cause is for the server log only;
// Detail is shown to the client in error.data.detail (it names the argument
// and the rule, never a stored value).
type Failure struct {
	Code   string
	Cause  error
	Detail string
}

func (f *Failure) Error() string {
	if f.Cause != nil {
		return f.Code + ": " + f.Cause.Error()
	}
	return f.Code
}

func (f *Failure) Unwrap() error { return f.Cause }

// Fail is a Failure with code only.
func Fail(code string) *Failure { return &Failure{Code: code} }

// FailDetail is Fail with a client-visible detail.
func FailDetail(code, detail string) *Failure { return &Failure{Code: code, Detail: detail} }
