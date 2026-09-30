package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// selfImproveCronSpec is FR-SELFIMPROVE-1's "日次（引け後）" trigger: 15:40
// JST (10 minutes after TSE's 15:30 close, non-functional.md §3), weekdays
// only, interpreted in selfImproveLocation, NOT the host's time.Local
// (robfig/cron's default). Holidays are not modelled: a holiday run yields
// no new Calibration data and so no proposal.
const selfImproveCronSpec = "40 15 * * 1-5"

// selfImproveLocation is JST (UTC+9, no DST): a fixed zone like
// marketcalendar.JST (not importable here, doc.go), so no IANA tzdata needed.
var selfImproveLocation = time.FixedZone("JST", 9*60*60)

// selfImproveSchedule parses selfImproveCronSpec pinned to selfImproveLocation;
// the other cron entries (e.g. the 16:00 backup) keep time.Local.
func selfImproveSchedule() (cron.Schedule, error) {
	sched, err := cron.ParseStandard(selfImproveCronSpec)
	if err != nil {
		return nil, err
	}
	sched.(*cron.SpecSchedule).Location = selfImproveLocation // a plain 5-field spec is always a *SpecSchedule
	return sched, nil
}

// selfImprovePayload is EnqueueSelfImprove's job payload. It carries no
// fields: the registered jobqueue.JobQueueAnalytics handler
// (internal/bootstrap's handleSelfImprove, running
// internal/service/selfimprove.Governor.RunDaily) derives "today's" analysis window itself, the same way
// fullScanPayload's per-instrument jobs need no more than InstrumentID/
// Symbol.
type selfImprovePayload struct{}

// EnqueueSelfImprove enqueues a single jobqueue.JobQueueAnalytics job
// due at now (functional.md §4.14 FR-SELFIMPROVE-1's daily Sol analysis
// batch): one global job, not one per instrument (unlike EnqueueFullScan),
// since Sol's analysis spans every instrument's recent Calibration data
// in a single pass.
func (s *Scheduler) EnqueueSelfImprove(ctx context.Context, now time.Time) error {
	payload, err := json.Marshal(selfImprovePayload{})
	if err != nil {
		return fmt.Errorf("scheduler: encode self-improve payload: %w", err)
	}
	if _, err := s.jobs.Enqueue(ctx, jobqueue.JobQueueAnalytics, string(payload), now); err != nil {
		return fmt.Errorf("scheduler: enqueue self-improve job: %w", err)
	}
	return nil
}
