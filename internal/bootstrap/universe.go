package bootstrap

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/universe"
)

// EnvUniversePath names the environment variable holding the path of the
// scan-universe master CSV (see universe.Parse). Unset or empty falls back
// to DefaultUniversePath next to the running executable.
const EnvUniversePath = "PITHA_UNIVERSE_PATH"

// DefaultUniversePath is the universe CSV looked up next to the running
// executable when EnvUniversePath is unset (same convention as config/*.yaml).
const DefaultUniversePath = "config/universe.csv"

// syncUniverse upserts the instruments master from the universe CSV so a
// clean database gets its scan universe (functional.md §7: 対象銘柄の取得;
// kabuステーションAPI cannot list listed stocks, so the operator supplies
// the file, environment/setup.md「銘柄マスタの投入」). It runs once at
// start-up, before the PUSH registration and the first full scan read the
// table, and is idempotent. Problems are logged, never fatal: an instruments
// table that already holds a universe keeps working, and an empty one is
// reported loudly because every scan then does nothing.
func (s *Services) syncUniverse(ctx context.Context) {
	path, ok := resolveConfigPath("", EnvUniversePath, DefaultUniversePath)
	if !ok {
		active, err := s.Instruments.ListActive(ctx)
		if err == nil && len(active) == 0 {
			slog.Error("bootstrap: scan universe is empty: no universe CSV found, so no instrument is scanned; set "+EnvUniversePath+" or place "+DefaultUniversePath+" next to the executable (environment/setup.md 銘柄マスタの投入)",
				"env", EnvUniversePath)
		}
		return
	}
	total, changed, err := universe.SyncFile(ctx, s.Instruments, path)
	if err != nil {
		slog.Error("bootstrap: universe sync failed, keeping the instruments table as is", "path", path, "error", err)
		return
	}
	slog.Info("bootstrap: universe synced", "path", path, "instruments", total, "changed", changed)
}
