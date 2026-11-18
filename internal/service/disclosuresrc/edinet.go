package disclosuresrc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/disclosuresrc"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosuresrc/edinet"
)

// EdinetChunkRequests is the most requests one EdinetRecorder.Run sends. A longer
// backlog is done over several runs (Result.More), so the maintenance runner
// never waits on one long run.
const EdinetChunkRequests = 5

// EdinetLister is what the recorder needs of the EDINET client;
// *edinet.Client implements it. Tests pass a fake.
type EdinetLister interface {
	// Configured reports whether the API key is set.
	Configured() bool
	// ListDocuments returns the document list of the JST day (YYYY-MM-DD).
	ListDocuments(ctx context.Context, day string) (edinet.List, error)
}

// EdinetRecorder records the EDINET document list of the last days (metadata
// only). A day's list is fetched once, then once more after the day is over
// (documents are submitted until the evening), then never again.
type EdinetRecorder struct {
	store    Store
	client   EdinetLister
	settings SettingsLoader
	now      func() time.Time
	pause    func(ctx context.Context, d time.Duration) error
}

// NewEdinetRecorder returns a recorder of client's lists into store.
func NewEdinetRecorder(store Store, client EdinetLister, settings SettingsLoader) *EdinetRecorder {
	return &EdinetRecorder{store: store, client: client, settings: settings, now: time.Now, pause: pauseFor}
}

// WithClock replaces the clock (tests).
func (r *EdinetRecorder) WithClock(now func() time.Time) *EdinetRecorder { r.now = now; return r }

// WithPause replaces the wait between two requests (tests).
func (r *EdinetRecorder) WithPause(p func(ctx context.Context, d time.Duration) error) *EdinetRecorder {
	r.pause = p
	return r
}

// Run does one bounded chunk. Switch off or no key: it returns at once with no
// request and no write. A failed request is recorded (code only) and ends the
// run quietly; it is not retried within the run.
func (r *EdinetRecorder) Run(ctx context.Context) (Result, error) {
	s, err := r.settings(ctx)
	if err != nil {
		return Result{}, err
	}
	if !s.EdinetEnabled {
		return Result{Disabled: true}, nil
	}
	if r.client == nil || !r.client.Configured() {
		return Result{NotConfigured: true}, nil
	}
	now := r.now()
	days := candidateDays(now, min(s.EdinetLookbackDays, s.RetentionDays))
	var res Result
	for _, day := range days {
		pending, err := r.pending(ctx, day, now)
		if err != nil {
			return res, err
		}
		if !pending {
			continue
		}
		if res.Requests >= EdinetChunkRequests {
			res.More = true
			break
		}
		if res.Requests > 0 {
			if err := r.pause(ctx, RequestInterval); err != nil {
				return res, err
			}
		}
		stop, err := r.fetchDay(ctx, day, s.EdinetDailyRequestCap, &res)
		if err != nil || stop {
			return res, err
		}
	}
	if !res.More {
		purged, err := r.store.Purge(ctx, now.AddDate(0, 0, -s.RetentionDays))
		if err != nil {
			return res, fmt.Errorf("disclosuresrc: edinet retention: %w", err)
		}
		res.Purged = purged
	}
	return res, nil
}

// fetchDay counts and sends one request for day and stores the list. stop is
// true when the run must end (the cap is used up or the request failed).
func (r *EdinetRecorder) fetchDay(ctx context.Context, day string, capPerDay int, res *Result) (stop bool, err error) {
	now := r.now()
	ok, err := r.store.BeginRequest(ctx, disclosuresrc.SourceEdinet, repo.Day(now), capPerDay, now)
	if err != nil {
		return true, fmt.Errorf("disclosuresrc: %w", err)
	}
	if !ok {
		res.CapReached = true
		return true, nil
	}
	res.Requests++
	list, err := r.client.ListDocuments(ctx, day)
	if err != nil {
		if ctx.Err() != nil {
			return true, ctx.Err()
		}
		res.Failures++
		code := failureCode(err)
		slog.WarnContext(ctx, "disclosuresrc: edinet request failed", "day", day, "code", code)
		if ferr := r.store.RecordFailure(ctx, disclosuresrc.SourceEdinet, code, r.now()); ferr != nil {
			return true, fmt.Errorf("disclosuresrc: %w", ferr)
		}
		return true, nil
	}
	at := r.now()
	n, err := r.store.UpsertEdinetDocuments(ctx, list.Docs, at)
	if err != nil {
		return true, fmt.Errorf("disclosuresrc: %w", err)
	}
	if err := r.store.MarkEdinetDay(ctx, disclosuresrc.EdinetDay{Day: day, FetchedAt: at, Total: list.Total, Recorded: n, Skipped: list.Skipped}); err != nil {
		return true, fmt.Errorf("disclosuresrc: %w", err)
	}
	if err := r.store.RecordSuccess(ctx, disclosuresrc.SourceEdinet, at); err != nil {
		return true, fmt.Errorf("disclosuresrc: %w", err)
	}
	res.Days++
	res.Rows += n
	return false, nil
}

// pending reports whether the list of day is still to fetch: never fetched, or
// (a past day) fetched only while the day was still running.
func (r *EdinetRecorder) pending(ctx context.Context, day string, now time.Time) (bool, error) {
	at, ok, err := r.store.EdinetDayFetchedAt(ctx, day)
	if err != nil {
		return false, fmt.Errorf("disclosuresrc: %w", err)
	}
	if !ok {
		return true, nil
	}
	return day < repo.Day(now) && repo.Day(at) <= day, nil
}

// candidateDays lists the weekdays (JST) of the last `lookback` days, today
// first. EDINET has no submissions on weekends, so those are not asked for.
func candidateDays(now time.Time, lookback int) []string {
	today := now.In(time.FixedZone("JST", 9*60*60))
	var out []string
	for i := range lookback {
		d := today.AddDate(0, 0, -i)
		if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue
		}
		out = append(out, d.Format(time.DateOnly))
	}
	return out
}

// failureCode is the short code of a request error (never its text).
func failureCode(err error) string {
	var re *edinet.RequestError
	if errors.As(err, &re) {
		return re.Code
	}
	return disclosuresrc.FailTransport
}
