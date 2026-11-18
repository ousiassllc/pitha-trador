// Package researchjobstatus reads how the two research-only nightly tasks are doing
// for the Settings screen's 研究用の日次評価の記録・学習・書き出し card: the daily
// record of the Jev evaluation (jev_evaluation_daily) and the research model's
// nightly training (research_model).
//
// It is display-only and read-only: it only reads runtime_settings (the two
// switches and the maintenance runner's per-task state, see maintenance.ReadStatus)
// and never writes, so a task that is off stays untouched. Nothing here may feed
// the trading decision, an order, the funds or a promotion.
package researchjobstatus

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// The maintenance runner's names of the two tasks (the keys of their state).
const (
	EvalTask  = "jev_evaluation_daily"
	ModelTask = "research_model"
)

// State is what a task is doing, as far as the operator can tell.
type State string

const (
	// Off: the switch is off, so the task is not running. This is not a failure.
	Off State = "off"
	// NotYetSucceeded: the switch is on but the task has neither succeeded nor failed yet.
	NotYetSucceeded State = "not_yet_succeeded"
	// Succeeded: the switch is on and the last run ended well.
	Succeeded State = "succeeded"
	// Failed: the switch is on and the last run failed.
	Failed State = "failed"
)

// Task is one task's status.
type Task struct {
	Name  string
	Label string
	State State
	// LastSuccessDate is the day ("2006-01-02") of the last success; empty before the first one.
	LastSuccessDate string
	// FailureDate and FailureReason describe the last failure while State is Failed
	// (the reason is one short line without secrets or bodies).
	FailureDate   string
	FailureReason string
}

// Store is the runtime_settings read access the service needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// stateStore adapts a read-only Store to maintenance.State for ReadStatus; Set is
// never called by ReadStatus and refuses, so this package cannot write.
type stateStore struct{ Store }

func (stateStore) Set(context.Context, string, string, time.Time) error {
	return fmt.Errorf("researchjobstatus: read-only")
}

// Service reads the status of the two tasks.
type Service struct{ store Store }

// New returns a Service reading from store.
func New(store Store) *Service { return &Service{store: store} }

// Tasks returns the status of the daily evaluation record, then of the research
// model. A failed read is an error: the caller must not guess the state.
func (s *Service) Tasks(ctx context.Context) ([]Task, error) {
	jobs := []struct {
		name, label string
		load        func(context.Context, researchjobsettings.Store) (researchjobsettings.Job, error)
	}{
		{EvalTask, "評価の日次記録", researchjobsettings.Eval},
		{ModelTask, "研究用モデルの夜間の学習", researchjobsettings.Model},
	}
	out := make([]Task, 0, len(jobs))
	for _, j := range jobs {
		job, err := j.load(ctx, s.store)
		if err != nil {
			return nil, fmt.Errorf("researchjobstatus: %w", err)
		}
		st, err := maintenance.ReadStatus(ctx, stateStore{s.store}, j.name)
		if err != nil {
			return nil, fmt.Errorf("researchjobstatus: %w", err)
		}
		// An earlier success stays visible while off; an old failure does not: off is not a failure.
		t := Task{Name: j.name, Label: j.label, State: Off, LastSuccessDate: st.LastSuccessDate}
		if job.Enabled {
			t.State = NotYetSucceeded
			switch {
			case st.Failure != nil:
				t.State, t.FailureDate, t.FailureReason = Failed, st.Failure.Date, maintenance.ShortReason(st.Failure.Reason)
			case st.LastSuccessDate != "":
				t.State = Succeeded
			}
		}
		out = append(out, t)
	}
	return out, nil
}
