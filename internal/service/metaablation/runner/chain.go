package runner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
)

// chainRun decides the run record of a new pre-registration m. A study is a
// database copy and a model: when out already holds a frozen pre-registration of
// the same study, this one is a changed value and MUST name the one it
// supersedes (-supersedes) and why (-new-run-reason): the run count goes on from
// it and the earlier run stays listed, so no rule value can be changed under a
// fresh run 1. The earlier manifest is never touched. Without any earlier one,
// -supersedes and -new-run-reason are refused (there is nothing to supersede).
func chainRun(out string, o InitOptions, m manifest.Manifest, now time.Time) (manifest.Run, error) {
	refuse := func(format string, args ...any) (manifest.Run, error) {
		return manifest.Run{}, fmt.Errorf("%w: runner: "+format, append([]any{ErrRefused}, args...)...)
	}
	sameStudy := func(e manifest.Manifest) bool {
		return e.DBCopy.SHA256 == m.DBCopy.SHA256 && e.Model.Version == m.Model.Version
	}
	latest, found, err := latestPrereg(out, sameStudy)
	if err != nil {
		return manifest.Run{}, err
	}
	if o.Supersedes == "" {
		if o.NewRunReason != "" {
			return refuse("-new-run-reason without -supersedes: name the earlier pre-registration this one replaces")
		}
		if found {
			return refuse("%s already holds a pre-registration of this database copy and model (run %d): a changed value is a new run; pass -supersedes <that manifest> and -new-run-reason", out, latest.Run.Count)
		}
		return manifest.Run{Count: 1}, nil
	}
	if o.NewRunReason == "" {
		return refuse("-supersedes needs -new-run-reason (why the study is repeated)")
	}
	prev, err := manifest.ReadFrozen(o.Supersedes)
	if err != nil {
		return manifest.Run{}, fmt.Errorf("%w: runner: -supersedes: %w", ErrRefused, err)
	}
	if !sameStudy(prev) {
		return refuse("-supersedes %s is of another database copy or model (%.12s / %s), this init is for %.12s / %s: only the same study continues a chain", o.Supersedes, prev.DBCopy.SHA256, prev.Model.Version, m.DBCopy.SHA256, m.Model.Version)
	}
	if found && latest.Run.Count > prev.Run.Count {
		return refuse("%s holds run %d of this study, later than the superseded run %d: supersede the latest one", out, latest.Run.Count, prev.Run.Count)
	}
	prevs := append([]manifest.PreviousRun(nil), prev.Run.Previous...)
	prevs = append(prevs, manifest.PreviousRun{Count: prev.Run.Count, SHA256: prev.Frozen.SHA256, FrozenAt: prev.Frozen.At, ChangeReason: o.NewRunReason})
	return manifest.Run{Count: prev.Run.Count + 1, Previous: prevs}, nil
}

// latestPrereg is the pre-registration of the highest run count under dir (any
// depth) that match accepts; unreadable or non-frozen files are skipped.
func latestPrereg(dir string, match func(manifest.Manifest) bool) (best manifest.Manifest, found bool, err error) {
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".prereg.manifest.json") {
			return nil
		}
		m, rerr := manifest.ReadFrozen(path)
		if rerr != nil || !match(m) {
			return nil
		}
		if !found || m.Run.Count > best.Run.Count {
			best, found = m, true
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) {
		return manifest.Manifest{}, false, fmt.Errorf("runner: scan %s for earlier pre-registrations: %w", dir, walkErr)
	}
	return best, found, nil
}
