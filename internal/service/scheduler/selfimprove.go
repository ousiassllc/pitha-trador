package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// selfImproveCronSpec is FR-SELFIMPROVE-1's "日次（引け後）" trigger: TSE's
// 15:30 JST close (non-functional.md §39 "9:00-11:30 / 12:30-15:30 JST"),
// expressed as 06:30 UTC (JST is UTC+9, no DST), weekdays only (the
// market is closed Sat/Sun, so there is nothing new to analyze). Like
// fullScanInterval's own trading-calendar gap (this package's Start doc
// comment, execution.MarketContext.MarketCloseAt's "no trading-calendar
// concept exists yet"), this does not account for JP market holidays: an
// occasional holiday run simply re-analyzes a day with no new Calibration
// data, producing no proposal (assist.Sol.Analyze's own "nothing
// actionable" outcome) rather than an incorrect one.
const selfImproveCronSpec = "30 6 * * 1-5"

// selfImprovePayload is EnqueueSelfImprove's job payload. It carries no
// fields: the registered repository.JobQueueAnalytics handler
// (internal/bootstrap's handleSelfImprove, running
// internal/service/selfimprove.Governor.RunDaily) derives "today's" analysis window itself, the same way
// fullScanPayload's per-instrument jobs need no more than InstrumentID/
// Symbol.
type selfImprovePayload struct{}

// EnqueueSelfImprove enqueues a single repository.JobQueueAnalytics job
// due at now (functional.md §4.14 FR-SELFIMPROVE-1's daily Sol analysis
// batch): one global job, not one per instrument (unlike EnqueueFullScan),
// since Sol's analysis spans every instrument's recent Calibration data
// in a single pass.
func (s *Scheduler) EnqueueSelfImprove(ctx context.Context, now time.Time) error {
	payload, err := json.Marshal(selfImprovePayload{})
	if err != nil {
		return fmt.Errorf("scheduler: encode self-improve payload: %w", err)
	}
	if _, err := s.jobs.Enqueue(ctx, repository.JobQueueAnalytics, string(payload), now); err != nil {
		return fmt.Errorf("scheduler: enqueue self-improve job: %w", err)
	}
	return nil
}
