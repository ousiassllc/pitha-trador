package runner

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime/debug"
	"strings"
)

// checkOutDir rejects an output directory that is the database copy itself or
// lies inside (or is) the directory the copy lives in: the report must never
// land next to the data it was computed from, where SQLite's side files and a
// later copy of the directory would mix with it. It returns the absolute path.
func checkOutDir(outDir, dbPath string) (string, error) {
	if outDir == "" {
		return "", errors.New("runner: -out is required (a directory outside the database copy's directory)")
	}
	out, err := filepath.Abs(outDir)
	if err != nil {
		return "", fmt.Errorf("runner: -out: %w", err)
	}
	db, err := filepath.Abs(dbPath)
	if err != nil {
		return "", fmt.Errorf("runner: -db: %w", err)
	}
	if out == db {
		return "", fmt.Errorf("runner: -out %s is the database file itself", out)
	}
	dbDir := filepath.Dir(db)
	if rel, err := filepath.Rel(dbDir, out); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("runner: -out %s is inside the database copy's directory %s", out, dbDir)
	}
	return out, nil
}

// CodeVersion identifies the code that wrote a manifest: the VCS revision of
// the build (with "-dirty" when modified), else "devel".
func CodeVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "devel"
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	switch {
	case rev == "":
		return "devel"
	case dirty:
		return rev + "-dirty"
	}
	return rev
}
