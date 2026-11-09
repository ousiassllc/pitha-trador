package runner

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/report"
)

// checkOutDir rejects an output directory that is the database copy itself or
// lies inside (or is) the directory the copy lives in: the report must never
// land next to the data it was computed from, where SQLite's side files and a
// later copy of the directory would mix with it. It returns the absolute path.
func checkOutDir(outDir, dbPath string) (string, error) {
	if outDir == "" {
		return "", fmt.Errorf("%w: runner: -out is required (a directory outside the database copy's directory)", ErrRefused)
	}
	// Symlinks are resolved (in the deepest existing ancestor of -out) before the
	// comparison: a link into the database's directory is inside it.
	out, err := report.ResolveExisting(outDir)
	if err != nil {
		return "", fmt.Errorf("runner: -out: %w", err)
	}
	db, err := report.ResolveExisting(dbPath)
	if err != nil {
		return "", fmt.Errorf("runner: -db: %w", err)
	}
	if out == db {
		return "", fmt.Errorf("%w: runner: -out %s is the database file itself", ErrRefused, out)
	}
	dbDir := filepath.Dir(db)
	if rel, err := filepath.Rel(dbDir, out); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: runner: -out %s is inside the database copy's directory %s", ErrRefused, out, dbDir)
	}
	return out, nil
}

// CodeVersion identifies the code that wrote a manifest: the VCS revision of
// the build (with "-dirty" when modified); without a build stamp (go run, go
// test) the working tree's `git rev-parse HEAD` as a best effort; else "devel".
func CodeVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		rev, dirty := "", false
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if rev != "" {
			if dirty {
				return rev + "-dirty"
			}
			return rev
		}
	}
	return gitHead()
}

// gitHead is the current commit of the working directory's repository (with
// "-dirty" when it has uncommitted changes), or "devel" when git cannot say.
func gitHead() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output()
	rev := strings.TrimSpace(string(out))
	if err != nil || rev == "" {
		return "devel"
	}
	if st, err := exec.CommandContext(ctx, "git", "status", "--porcelain", "--untracked-files=no").Output(); err == nil && len(strings.TrimSpace(string(st))) > 0 {
		return rev + "-dirty"
	}
	return rev
}
