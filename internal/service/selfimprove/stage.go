package selfimprove

import "fmt"

// AI stage names reported in Notifier.AIStageSkipped.
const (
	stageSol  = "sol"
	stageOpus = "opus"
)

// aiStageError marks a failure of the external Sol/Opus API (as opposed to
// a local/database failure): RunDaily skips that stage for the day, notifies
// and retries the next business day (overview.md §8) instead of failing the
// whole batch.
type aiStageError struct {
	stage string
	err   error
}

func (e *aiStageError) Error() string { return fmt.Sprintf("selfimprove: %s api: %v", e.stage, e.err) }

func (e *aiStageError) Unwrap() error { return e.err }
