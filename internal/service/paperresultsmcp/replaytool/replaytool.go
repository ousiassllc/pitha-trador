// Package replaytool holds the arguments, descriptions and entry points of the
// replay evaluation tools of the Paper results MCP (issue #1085;
// docs/api/endpoints/paper-results-mcp-jev-replay.md): replay_estimate,
// replay_start, replay_stop, replay_progress and replay_results.
//
// replay_start and replay_stop are the only tools here that write, and they
// write only the replay run table (jev_replay_runs): a start inserts a pending
// run, a stop sets its stop flag. The runner inside the application does the
// work. No tool writes a trading setting or the replay switch
// (jev.replay.enabled stays on the Settings screen: like jev.trial.enabled it is
// not on the MCP's settings_set whitelist), none sends a request itself, and
// none returns an API key, a response body or an error text.
package replaytool

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	domainreplay "github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/service/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// The tools' failures. The Paper results MCP maps each to a spec error; none
// carries a secret.
var (
	// ErrDisabled: jev.replay.enabled is off (or unreadable). Only the Settings
	// screen turns it on.
	ErrDisabled = errors.New("replaytool: the replay switch is off")
	// ErrActiveRun: another run is pending or running; one run at a time.
	ErrActiveRun = errors.New("replaytool: another replay run is active")
	// ErrStaleEstimate: the confirm token is not the token of the current estimate.
	ErrStaleEstimate = errors.New("replaytool: the estimate changed or was not confirmed")
	// ErrNothingToReplay: the estimate has no call to make.
	ErrNothingToReplay = errors.New("replaytool: the estimate has no call to make")
	// ErrNotFound: no run with that id.
	ErrNotFound = errors.New("replaytool: replay run not found")
	// ErrNotActive: the run is already finished (it cannot be stopped).
	ErrNotActive = errors.New("replaytool: replay run is not active")
)

// Reason returns the operator-facing reason of a rejected request (it names the
// argument and the rule, never a value of a secret), and whether err is one.
func Reason(err error) (string, bool) {
	if !errors.Is(err, jevreplay.ErrInvalidRequest) {
		return "", false
	}
	return strings.TrimPrefix(err.Error(), jevreplay.ErrInvalidRequest.Error()+": "), true
}

// PlanArgs are the arguments of replay_estimate and replay_start besides the
// period (from, to: the live decisions' timestamps, JST days).
type PlanArgs struct {
	// DecisionType is "trader" (the default and the only one).
	DecisionType string `json:"decision_type"`
	// MaxRows is the most live decisions the run covers: required, so a run is
	// always bounded by the caller (1 to domain/jevreplay.MaxRows).
	MaxRows *int `json:"max_rows"`
	// Arms are arm ids of jev.trial.arms; empty means every configured arm.
	Arms []string `json:"arms"`
}

// RunIDArgs is {"run_id": n} (required or optional by the tool).
type RunIDArgs struct {
	RunID *int64 `json:"run_id"`
}

// Validate checks run_id: present when required, 1 or more when given.
func (a RunIDArgs) Validate(required bool) error {
	if (required && a.RunID == nil) || (a.RunID != nil && *a.RunID < 1) {
		return fmt.Errorf("%w: run_id must be 1 or more", jevreplay.ErrInvalidRequest)
	}
	return nil
}

// Validate checks the arguments that need no database.
func (a PlanArgs) Validate() error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{jevreplay.ErrInvalidRequest}, args...)...)
	}
	if a.MaxRows == nil || *a.MaxRows < 1 || *a.MaxRows > domainMaxRows {
		return invalid("max_rows is required and must be 1〜%d", domainMaxRows)
	}
	if len(a.Arms) > jevtrialsettings.MaxArms {
		return invalid("at most %d arms", jevtrialsettings.MaxArms)
	}
	for _, id := range a.Arms {
		if !jevtrialsettings.ValidArmID(id) {
			return invalid("an arm id is invalid")
		}
	}
	if len(slices.Compact(slices.Sorted(slices.Values(a.Arms)))) != len(a.Arms) {
		return invalid("an arm is named twice")
	}
	return nil
}

// domainMaxRows is the hard bound of one run (jev_replay_runs.max_rows).
const domainMaxRows = domainreplay.MaxRows

// Request is the run request the arguments and the resolved period make.
func (a PlanArgs) Request(r query.Range) jevreplay.Request {
	return jevreplay.Request{DecisionType: a.DecisionType, From: r.From, To: r.To, MaxRows: *a.MaxRows, Arms: a.Arms}
}

// formatTime renders t as RFC3339 UTC for a response.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := formatTime(*t)
	return &s
}

// Failure is the spec error a replay tool error maps to: Invalid is true for
// invalid_argument and false for invalid_value; Detail names the rule, never a
// value of a secret. ok is false for an error that is none of the tools' own.
func Failure(err error) (invalid bool, detail string, ok bool) {
	if reason, ok := Reason(err); ok {
		return true, reason, true
	}
	switch {
	case errors.Is(err, ErrDisabled):
		return false, "リプレイはオフです（jev.replay.enabled）。Settings 画面でオンにしてください（MCP からはオンにできません）", true
	case errors.Is(err, ErrStaleEstimate):
		return false, "confirm_token が見積もりと一致しません。同じ引数で replay_estimate を取り直し、その confirm_token を渡してください", true
	case errors.Is(err, ErrNothingToReplay):
		return false, "見積もりに呼び出しがありません（対象の判断がない、またはすべてアームの学習カットオフ以前です）", true
	case errors.Is(err, ErrActiveRun):
		return false, "ほかのリプレイが待ち状態か実行中です。replay_progress で確認し、終わるか replay_stop で止めてから開始してください", true
	case errors.Is(err, ErrNotFound):
		return true, "その run_id のリプレイはありません。replay_progress で確認してください", true
	case errors.Is(err, ErrNotActive):
		return false, "このリプレイはもう終わっています（止める必要はありません）", true
	}
	return false, "", false
}
