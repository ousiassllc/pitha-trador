// Package dailyguardseed runs the one-time Paper initial values of the daily
// entry-halt rules (issue #782) at startup.
package dailyguardseed

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
)

// Run stores the Paper initial values once per database (see
// dailyguardsettings.SeedPaperDefaults). They are only a rough guide the
// owner must review before Live. A failure is logged and never stops
// startup: the rules then simply stay as stored (off when nothing is).
//
// Premise: this build is Paper-only (internal/bootstrap/risk.go). Gate the
// call on Paper before any Live build is added.
func Run(ctx context.Context, store dailyguardsettings.SeedStore, now time.Time) {
	if err := dailyguardsettings.SeedPaperDefaults(ctx, store, now); err != nil {
		slog.Warn("bootstrap: seed daily guard Paper defaults failed; the rules stay as stored", "error", err)
	}
}
