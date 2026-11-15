// Package backtestview renders the rows of backtest_runs as the output of the
// research-backtest tools (issue #1086; docs/api/endpoints/paper-results-mcp-backtest.md).
package backtestview

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/backtestrun"
	"github.com/ousiassllc/pitha-trador/internal/service/backtestlab"
)

// Run is one run as backtest_start and backtest_list return it.
type Run struct {
	RunID         int64   `json:"run_id"`
	Status        string  `json:"status"`
	CreatedAt     string  `json:"created_at"`
	StartedAt     *string `json:"started_at"`
	FinishedAt    *string `json:"finished_at"`
	Period        Period  `json:"period"`
	ReplayMode    string  `json:"replay_mode"`
	CodeVersion   string  `json:"code_version"`
	ExploredCount int     `json:"explored_count"`
	ErrorCode     *string `json:"error_code"`
	Caution       string  `json:"caution"`
}

// Period is the requested period (JST dates, [from, to)) and the
// data actually found in it.
type Period struct {
	From            string  `json:"from"`
	To              string  `json:"to"`
	FirstSnapshotAt *string `json:"first_snapshot_at"`
	LastSnapshotAt  *string `json:"last_snapshot_at"`
}

// Detail is backtest_get's data: the view, what was asked for and,
// once the run succeeded, the result of every parameter set.
type Detail struct {
	Run
	ExtraCostBps      []float64                   `json:"extra_cost_bps"`
	RequestedVariants []backtestlab.Variant       `json:"requested_variants"`
	Variants          []backtestlab.VariantResult `json:"variants"`
}

// NewRun renders run. A run that is queued or running but was
// created by another process (owner differs) is shown as failed / interrupted:
// the process that was running it is gone, whatever the row still says. The
// row itself is corrected by the next backtest_start (backtestjob).
func NewRun(run backtestrun.Run, owner string) Run {
	status, code, finished := run.Status, run.ErrorCode, run.FinishedAt
	if (status == backtestrun.StatusQueued || status == backtestrun.StatusRunning) && run.Owner != owner {
		status, code, finished = backtestrun.StatusFailed, backtestrun.ErrorInterrupted, nil
	}
	v := Run{
		RunID: run.ID, Status: status, CreatedAt: rfc3339(&run.CreatedAt), StartedAt: rfc3339Ptr(run.StartedAt), FinishedAt: rfc3339Ptr(finished),
		Period: Period{
			From: run.FromDate, To: run.ToDate, FirstSnapshotAt: rfc3339Ptr(run.FirstSnapshotAt), LastSnapshotAt: rfc3339Ptr(run.LastSnapshotAt),
		},
		ReplayMode: run.ReplayMode, CodeVersion: run.CodeVersion, ExploredCount: run.ExploredCount, Caution: backtestlab.ReferenceCaution,
	}
	if code != "" {
		v.ErrorCode = &code
	}
	return v
}

// NewDetail renders run with its request and result.
func NewDetail(run backtestrun.Run, owner string) (Detail, error) {
	d := Detail{Run: NewRun(run, owner)}
	var req backtestlab.Request
	if err := json.Unmarshal([]byte(run.RequestJSON), &req); err != nil {
		return d, fmt.Errorf("backtest run %d: stored request: %w", run.ID, err)
	}
	d.ExtraCostBps, d.RequestedVariants = req.ExtraCostBps, req.Variants
	if run.ResultJSON != "" {
		var res backtestlab.Result
		if err := json.Unmarshal([]byte(run.ResultJSON), &res); err != nil {
			return d, fmt.Errorf("backtest run %d: stored result: %w", run.ID, err)
		}
		d.Variants = res.Variants
	}
	return d, nil
}

func rfc3339(t *time.Time) string { return t.UTC().Format(time.RFC3339) }

func rfc3339Ptr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := rfc3339(t)
	return &s
}
