package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	failureKeyFm = "system.maintenance.%s.last_failure"
	// MaxFailureReasonRunes bounds the stored reason: it is an operator hint, not a log.
	MaxFailureReasonRunes = 120
	// failureCleared is what a recovered task's failure row holds (State has no delete).
	failureCleared = "null"
)

// Failure is the last failed run of a task that has Task.TrackFailure set.
type Failure struct {
	// Date is the day of the failure in the runner's local time ("2006-01-02").
	Date string `json:"date"`
	// Reason is a short single-line summary of the error (see FailureReason).
	Reason string `json:"reason"`
}

// Status is what a runner has persisted about one task: the day of its last
// success (empty before the first one) and, until the task makes progress
// again, its last failure (nil when there is none). It is read-only
// information for the operator; nothing may take trading decisions from it.
type Status struct {
	LastSuccessDate string
	Failure         *Failure
}

// ReadStatus reads task name's persisted Status from state. A row that cannot be
// parsed counts as absent; only a failed read is an error.
func ReadStatus(ctx context.Context, state State, name string) (Status, error) {
	var s Status
	raw, ok, err := state.Get(ctx, fmt.Sprintf(settingKeyFm, name))
	if err != nil {
		return Status{}, fmt.Errorf("maintenance: read last success of %q: %w", name, err)
	}
	if ok {
		var day string
		if json.Unmarshal([]byte(raw), &day) == nil {
			s.LastSuccessDate = day
		}
	}
	f, err := readFailure(ctx, state, name)
	if err != nil {
		return Status{}, err
	}
	s.Failure = f
	return s, nil
}

func readFailure(ctx context.Context, state State, name string) (*Failure, error) {
	raw, ok, err := state.Get(ctx, fmt.Sprintf(failureKeyFm, name))
	if err != nil {
		return nil, fmt.Errorf("maintenance: read last failure of %q: %w", name, err)
	}
	if !ok {
		return nil, nil
	}
	var f *Failure
	if json.Unmarshal([]byte(raw), &f) != nil || f == nil || f.Date == "" {
		return nil, nil
	}
	return f, nil
}

// FailureReason is err's message as ShortReason.
func FailureReason(err error) string { return ShortReason(err.Error()) }

// ShortReason is s as a single line of at most MaxFailureReasonRunes runes:
// whitespace (newlines included) is collapsed and the rest is cut with an
// ellipsis, so a long error (e.g. one carrying a response body) is never
// stored or shown whole.
func ShortReason(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= MaxFailureReasonRunes {
		return s
	}
	return string([]rune(s)[:MaxFailureReasonRunes]) + "…"
}

// recordFailure stores the failed run of a task with TrackFailure set. A store
// error is logged only: a failing task must not fail twice.
func (r *Runner) recordFailure(ctx context.Context, task Task, now time.Time, cause error) {
	if !task.TrackFailure {
		return
	}
	raw, _ := json.Marshal(Failure{Date: now.Format(dayLayout), Reason: FailureReason(cause)})
	if err := r.state.Set(ctx, fmt.Sprintf(failureKeyFm, task.Name), string(raw), now); err != nil {
		slog.Error("scheduler: persist maintenance failure failed", "task", task.Name, "error", err)
	}
}

// clearFailure ends the stored failure of a task with TrackFailure set once it
// has made progress or succeeded. It writes only while a failure is stored.
func (r *Runner) clearFailure(ctx context.Context, task Task, now time.Time) {
	if !task.TrackFailure {
		return
	}
	f, err := readFailure(ctx, r.state, task.Name)
	if err != nil || f == nil {
		return
	}
	if err := r.state.Set(ctx, fmt.Sprintf(failureKeyFm, task.Name), failureCleared, now); err != nil {
		slog.Error("scheduler: clear maintenance failure failed", "task", task.Name, "error", err)
	}
}
