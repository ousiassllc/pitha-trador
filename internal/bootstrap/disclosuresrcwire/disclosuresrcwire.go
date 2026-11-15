// Package disclosuresrcwire is the composition root's wiring of the extra
// disclosure / news sources (issue #1096): a daily maintenance task that records
// the EDINET document list, and the constructor of the 立花 news task.
//
// Both are research-only and record-only (internal/service/disclosuresrc); both
// are OFF by default (disclosuresrcsettings): while a source's switch is off, or
// its key is not set, Run returns maintenance.ErrSkipped before any request or
// write, not even the runner's last-success date, so a default install gains
// no request and no write from this package.
//
// Only the EDINET task is registered by the bootstrap (Tasks). The 立花 news
// task (NewTachibanaNewsTask) is deliberately NOT: the 立花 news API is not
// measured on a real device yet (issue #725), the production build has no
// NewsFetcher, and nothing in the build can reach 立花 from here. It is
// registered only once #725 is done and a fetcher exists.
package disclosuresrcwire

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/disclosuresrcsettings"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosuresrc"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosuresrc/edinet"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

// Task names (the last-success date is stored under them) and labels.
const (
	EdinetTaskName = "disclosure_src_edinet"
	EdinetLabel    = "EDINET 書類一覧の記録"

	TachibanaNewsTaskName = "disclosure_src_tachibana_news"
	TachibanaNewsLabel    = "立花ニュースの記録"
)

// RunFrom is the JST time of day from which the tasks run: EDINET's day is
// mostly submitted by the evening, so the day's list is fetched after it.
const RunFrom = 18 * time.Hour

var jst = time.FixedZone("JST", 9*60*60)

// recorder is the runnable part of a source's recorder.
type recorder interface {
	Run(ctx context.Context) (disclosuresrc.Result, error)
}

// Job is one source's maintenance task.
type Job struct {
	name, label string
	rec         recorder
	now         func() time.Time
}

// Task returns the maintenance task for scheduler.WithMaintenanceTask.
func (j *Job) Task() maintenance.Task {
	return maintenance.Task{Name: j.name, Label: j.label, Run: j.Run}
}

// Run does one chunk once the JST time of day has reached RunFrom and the
// source is switched on with its key set; otherwise it returns
// maintenance.ErrSkipped (neither a success nor a failure; nothing is sent or
// written). A chunk that leaves work behind returns maintenance.ErrMore. The
// recording is by-product research data, so a panic is returned as an error
// instead of crashing the process that also trades. A failed request of the
// source is recorded in its status row (code only) and is not an error of the
// task: it is retried on the next day's run, never in a loop.
func (j *Job) Run(ctx context.Context) (err error) {
	n := j.now().In(jst)
	if time.Duration(n.Hour())*time.Hour+time.Duration(n.Minute())*time.Minute < RunFrom {
		return maintenance.ErrSkipped
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("disclosuresrcwire: panic while recording %s: %v", j.name, p)
		}
	}()
	res, err := j.rec.Run(ctx)
	if err != nil {
		return fmt.Errorf("disclosuresrcwire: %s: %w", j.name, err)
	}
	if res.Disabled || res.NotConfigured {
		return maintenance.ErrSkipped
	}
	slog.InfoContext(ctx, "disclosuresrcwire: recorded", "task", j.name, "requests", res.Requests, "failures", res.Failures,
		"rows", res.Rows, "purged_rows", res.Purged, "cap_reached", res.CapReached, "more", res.More)
	if res.More {
		return maintenance.ErrMore
	}
	return nil
}

func loader(settings *system.RuntimeSettingsRepository) disclosuresrc.SettingsLoader {
	return func(ctx context.Context) (disclosuresrcsettings.Settings, error) {
		return disclosuresrcsettings.Load(ctx, settings)
	}
}

// NewEdinetJob returns the EDINET document-list job of db. apiKey is the stored
// EDINET_API_KEY ("" = not set: the job sends nothing); baseURL is an optional
// override ("" = the public API). The key is held by the client only.
func NewEdinetJob(db *sql.DB, settings *system.RuntimeSettingsRepository, apiKey, baseURL string) *Job {
	client := edinet.New(edinet.Config{APIKey: apiKey, BaseURL: baseURL})
	j := &Job{name: EdinetTaskName, label: EdinetLabel, now: time.Now}
	j.rec = disclosuresrc.NewEdinetRecorder(repo.NewRepository(db), client, loader(settings)).WithClock(func() time.Time { return j.now() })
	return j
}

// Tasks returns the maintenance tasks the bootstrap registers: the EDINET one.
// The 立花 news task is not among them (see the package comment).
func Tasks(db *sql.DB, settings *system.RuntimeSettingsRepository, edinetAPIKey string) []maintenance.Task {
	return []maintenance.Task{NewEdinetJob(db, settings, edinetAPIKey, "").Task()}
}

// NewTachibanaNewsJob returns the 立花 news job over fetcher and creds. It is
// not registered by the bootstrap and no production code calls it: it waits for
// issue #725 (the real-device measurement of the 立花 news API).
func NewTachibanaNewsJob(db *sql.DB, settings *system.RuntimeSettingsRepository, fetcher disclosuresrc.NewsFetcher, creds disclosuresrc.CredentialHolder) *Job {
	return &Job{name: TachibanaNewsTaskName, label: TachibanaNewsLabel, now: time.Now,
		rec: disclosuresrc.NewTachibanaRecorder(repo.NewRepository(db), fetcher, creds, loader(settings))}
}
