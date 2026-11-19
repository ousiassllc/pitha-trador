// Package disclosuresrc records the extra disclosure / news sources (issue
// #1096): the EDINET document list (metadata only, EdinetRecorder) and the 立花
// e支店 news (headline, time, related symbols; TachibanaRecorder).
//
// Both are research-only and record-only. Nothing recorded here is an input of
// the gates, Policy, Risk, Jev, the RAG index, the calibration, the meta-model
// training or the existing disclosure skip rules (internal/service/disclosureskip,
// which keeps reading only disclosure_items). Both are OFF by default
// (internal/config/disclosuresrcsettings): while a source is off, or its key /
// credential is not set, it sends no request and writes nothing.
//
// The 立花 news recorder is written against the NewsFetcher interface only and is
// not wired to production: it waits for the 立花 real-device measurement (issue
// #725). Tests drive it with fakes.
package disclosuresrc

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/disclosuresrcsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/disclosuresrc"
)

// Store is the persistence the recorders need; *repository/disclosuresrc.Repository
// implements it.
type Store interface {
	BeginRequest(ctx context.Context, source, day string, capPerDay int, now time.Time) (bool, error)
	RecordSuccess(ctx context.Context, source string, now time.Time) error
	RecordFailure(ctx context.Context, source, code string, now time.Time) error
	UpsertEdinetDocuments(ctx context.Context, docs []disclosuresrc.EdinetDocument, now time.Time) (int, error)
	MarkEdinetDay(ctx context.Context, d disclosuresrc.EdinetDay) error
	EdinetDayFetchedAt(ctx context.Context, day string) (time.Time, bool, error)
	UpsertTachibanaNews(ctx context.Context, news []disclosuresrc.TachibanaNews, now time.Time) (int, error)
	Purge(ctx context.Context, cutoff time.Time) (int64, error)
}

// SettingsLoader reads the effective settings at every run, so a change on the
// Settings screen applies to the next run.
type SettingsLoader func(ctx context.Context) (disclosuresrcsettings.Settings, error)

// Result says what one Run did. Exactly one of Disabled / NotConfigured / the
// counters is meaningful: a disabled or unconfigured source did nothing at all
// (no request, no write).
type Result struct {
	// Disabled: the source's switch is off. NotConfigured: its key / credential
	// (or fetcher) is not set.
	Disabled      bool
	NotConfigured bool
	// Requests is the number of requests sent, Failures how many of them failed.
	Requests int
	Failures int
	// CapReached: the daily request cap stopped the run.
	CapReached bool
	// Days (EDINET) is the number of document-list days stored, Rows the rows written.
	Days int
	Rows int
	// Purged is the rows the retention deleted.
	Purged int64
	// More: the run did a bounded chunk and work is left (call again).
	More bool
}

// RequestInterval is the pause between two requests of one run: the sources
// are public or account-bound services, so they are asked slowly.
const RequestInterval = time.Second

// pauseFor waits d or until ctx is done.
func pauseFor(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
