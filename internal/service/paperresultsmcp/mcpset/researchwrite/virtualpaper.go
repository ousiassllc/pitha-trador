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
// two transactions on virtual_paper_profiles: CreateVirtualPaperProfile (one
// INSERT) and StopVirtualPaperProfile (one UPDATE). Both run behind the write
// switch. The days and trades of a profile are written only by the
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
