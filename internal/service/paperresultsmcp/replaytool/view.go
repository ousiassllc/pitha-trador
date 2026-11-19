package replaytool

import (
	"context"
	"errors"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	domainreplay "github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Limits of replay_progress's list.
const (
	DefaultListLimit = 10
	MaxListLimit     = 50
)

// ArmView is one arm of a run as it was configured when the run started (never
// a key).
type ArmView struct {
	ArmID           string  `json:"arm_id"`
	Model           string  `json:"model"`
	PromptVersion   string  `json:"prompt_version"`
	LLMCutoff       *string `json:"llm_cutoff"`
	LLMCutoffStatus string  `json:"llm_cutoff_status"`
	LLMCutoffLabel  string  `json:"llm_cutoff_label"`
	ReferenceOnly   bool    `json:"reference_only"`
}

// ProgressView is a run's progress: planned calls (the estimate), recorded
// calls (rows, failures included) and the cost counted so far.
type ProgressView struct {
	CallsPlanned   int     `json:"calls_planned"`
	CallsDone      int     `json:"calls_done"`
	CallsFailed    int     `json:"calls_failed"`
	CallsRemaining int     `json:"calls_remaining"`
	CountedCostUSD float64 `json:"counted_cost_usd"`
	MaxCostUSD     float64 `json:"max_cost_usd"`
}

// RunView is one run as the tools return it: its status, why it ended or
// waits, the spec, the estimate it was started with and its progress. No row,
// response or error text, no key.
type RunView struct {
	RunID  int64  `json:"run_id"`
	Status string `json:"status"`
	// StopRequested is the stop flag. StopReason is why a stopped/failed run
	// ended; BlockedReason is why a running run makes no call now (a daily cap, a
	// stop, a cooldown, the switch): it continues by itself when that clears.
	StopRequested bool              `json:"stop_requested"`
	StopReason    string            `json:"stop_reason"`
	BlockedReason string            `json:"blocked_reason"`
	Spec          domainreplay.Spec `json:"spec"`
	Arms          []ArmView         `json:"arms"`
	Decisions     int               `json:"decisions"`
	ListPriceUSD  *float64          `json:"list_price_usd"`
	Progress      ProgressView      `json:"progress"`
	CreatedBy     string            `json:"created_by"`
	CreatedAt     string            `json:"created_at"`
	StartedAt     *string           `json:"started_at"`
	FinishedAt    *string           `json:"finished_at"`
	UpdatedAt     string            `json:"updated_at"`
}

// View renders run for a response.
func View(run domainreplay.Run) RunView {
	v := RunView{
		RunID: run.ID, Status: run.Status, StopRequested: run.StopRequested, StopReason: run.StopReason, BlockedReason: run.BlockedReason,
		Spec: run.Spec, Arms: []ArmView{}, Decisions: run.Estimate.Decisions, ListPriceUSD: run.Estimate.ListPriceUSD,
		Progress: ProgressView{
			CallsPlanned: run.Estimate.Calls, CallsDone: run.CallsDone, CallsFailed: run.CallsFailed,
			CallsRemaining: max(0, run.Estimate.Calls-run.CallsDone), CountedCostUSD: run.CountedCostUSD, MaxCostUSD: run.Estimate.MaxCostUSD,
		},
		CreatedBy: run.CreatedBy, CreatedAt: formatTime(run.CreatedAt), StartedAt: formatTimePtr(run.StartedAt),
		FinishedAt: formatTimePtr(run.FinishedAt), UpdatedAt: formatTime(run.UpdatedAt),
	}
	if arms, err := jevtrialsettings.ParseArms(run.ArmsJSON); err == nil {
		for _, a := range arms {
			status := domainreplay.CutoffUnverified
			if _, known := a.TargetableFrom(); known {
				status = domainreplay.CutoffVerified
			}
			v.Arms = append(v.Arms, ArmView{
				ArmID: a.ID, Model: a.Model, PromptVersion: a.PromptVersion, LLMCutoff: cutoffDate(a),
				LLMCutoffStatus: status, LLMCutoffLabel: domainreplay.CutoffLabel(status), ReferenceOnly: status != domainreplay.CutoffVerified,
			})
		}
	}
	return v
}

// ProgressList is replay_progress's data.
type ProgressList struct {
	Runs []RunView `json:"runs"`
}

// Progress is replay_progress: one run (runID set) or the newest runs (limit,
// DefaultListLimit when 0), newest first. An unknown run is ErrNotFound.
func Progress(ctx context.Context, q query.Q, runID *int64, limit int) (ProgressList, error) {
	if runID != nil {
		run, err := repo.GetRun(ctx, q, *runID)
		if errors.Is(err, repo.ErrRunNotFound) {
			return ProgressList{}, ErrNotFound
		}
		if err != nil {
			return ProgressList{}, err
		}
		return ProgressList{Runs: []RunView{View(run)}}, nil
	}
	if limit <= 0 {
		limit = DefaultListLimit
	}
	runs, err := repo.ListRuns(ctx, q, limit)
	if err != nil {
		return ProgressList{}, err
	}
	out := ProgressList{Runs: make([]RunView, len(runs))}
	for i, r := range runs {
		out.Runs[i] = View(r)
	}
	return out, nil
}
