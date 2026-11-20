package researchwrite

import (
	"context"
	"database/sql"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/virtualpaper"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/virtualpaper"
)

// The virtual Paper side of the Writer (issue #1087). Its whole write surface is
// three transactions behind the write switch: CreateVirtualPaperProfile (one
// INSERT) and StopVirtualPaperProfile (one UPDATE) on virtual_paper_profiles,
// and RerunVirtualPaperFailedDay (one DELETE of a failed virtual_paper_days
// mark, issue #1131). The days and trades of a profile are written only by the
// application's daily recorder.

// CreateVirtualPaperProfile stores a profile (virtualpaper.Create's validation,
// caps and clone rule), behind the write switch (ErrWriteDisabled while it is
// off). client is the caller's host.
func CreateVirtualPaperProfile(ctx context.Context, w *mcpset.Writer, client string, args virtualpaper.CreateArgs) (repo.Profile, error) {
	var out repo.Profile
	err := w.InTx(ctx, "virtual paper profile transaction", true, func(tx *sql.Tx, now time.Time) error {
		var err error
		out, err = virtualpaper.Create(ctx, tx, now, client, args)
		return err
	})
	return out, err
}

// StopVirtualPaperProfile stops profile id, behind the write switch. changed is
// false when it was already stopped; the recorded data stays.
func StopVirtualPaperProfile(ctx context.Context, w *mcpset.Writer, id int64) (repo.Profile, bool, error) {
	var (
		out     repo.Profile
		changed bool
	)
	err := w.InTx(ctx, "virtual paper stop transaction", true, func(tx *sql.Tx, now time.Time) error {
		var err error
		out, changed, err = virtualpaper.Stop(ctx, tx, now, id)
		return err
	})
	return out, changed, err
}

// RerunVirtualPaperFailedDay makes profile id's failed day re-run-eligible
// (virtualpaper.RerunFailedDay's rules), behind the write switch
// (ErrWriteDisabled while it is off, with nothing written). The next daily
// record replays the day.
func RerunVirtualPaperFailedDay(ctx context.Context, w *mcpset.Writer, id int64, day string) error {
	return w.InTx(ctx, "virtual paper rerun transaction", true, func(tx *sql.Tx, now time.Time) error {
		return virtualpaper.RerunFailedDay(ctx, tx, now, id, day)
	})
}
