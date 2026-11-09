package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/loader"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/report"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// RunOptions are the inputs of `run`.
type RunOptions struct {
	DBPath, ManifestPath, OutDir string
	// NewRunReason, when set, makes this a NEW run: a copy of the frozen
	// manifest with Minimums laid over it (only the fields that are set), the
	// run count incremented, the earlier run listed with this reason, frozen
	// and written before anything is evaluated. Without it the manifest is
	// executed exactly as frozen and Minimums must be empty.
	NewRunReason string
	Minimums     manifest.Minimums
	// Now is the clock (time.Now when nil); it only stamps a new run.
	Now func() time.Time
}

// Summary is what a run produced.
type Summary struct {
	Manifest manifest.Manifest
	// Files are the report files; PreRegistration is the frozen manifest of a
	// new run ("" when the manifest was executed as it was).
	Files           report.Files
	PreRegistration string
	Label, Replay   LayerStatus
}

// LayerStatus is whether a layer ran, and why not when it did not.
type LayerStatus struct {
	Available bool
	Reason    string
}

// Run executes the frozen manifest on the read-only database copy and writes
// the JSON and Markdown report and the manifest into OutDir. It fails loudly
// when the manifest is not a valid frozen one, when the copy is not the one
// the manifest names (SHA-256), when the model the manifest froze is not the
// stored one, and when the copy changed during the run. A layer that cannot run
// is reported with its reason and the other layer's result is kept.
func Run(ctx context.Context, o RunOptions) (Summary, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	m, err := manifest.ReadFrozen(o.ManifestPath)
	if err != nil {
		return Summary{}, err
	}
	out, err := checkOutDir(o.OutDir, o.DBPath)
	if err != nil {
		return Summary{}, err
	}
	var sum Summary
	if o.NewRunReason != "" {
		if m, err = newRun(m, o); err != nil {
			return Summary{}, err
		}
	} else if len(o.Minimums.UnsetFields()) < 5 {
		return Summary{}, fmt.Errorf("%w: runner: a pre-registered value is changed only by a new run: give -new-run-reason with it", ErrRefused)
	}
	if err := checkDB(o.DBPath, m); err != nil {
		return Summary{}, err
	}
	if o.NewRunReason != "" {
		if sum.PreRegistration, err = writePreRegistration(out, m); err != nil {
			return Summary{}, err
		}
	}
	src, err := loader.Open(o.DBPath)
	if err != nil {
		return Summary{}, err
	}
	defer func() { _ = src.Close() }()
	doc, err := evaluate(ctx, m, src)
	if err != nil {
		return Summary{}, err
	}
	if err := checkDB(o.DBPath, m); err != nil {
		return Summary{}, fmt.Errorf("runner: the database copy changed during the run: %w", err)
	}
	if sum.Files, err = report.WriteFiles(out, doc, filepath.Dir(o.DBPath)); err != nil {
		return Summary{}, err
	}
	sum.Manifest = m
	sum.Label = LayerStatus{doc.Label.Available, doc.Label.Reason}
	sum.Replay = LayerStatus{doc.Replay.Available, doc.Replay.Reason}
	return sum, nil
}

// checkDB fails when the file is not the copy the manifest names.
func checkDB(path string, m manifest.Manifest) error {
	if err := loader.CheckCheckpointed(path); err != nil {
		return fmt.Errorf("%w: %w", ErrRefused, err)
	}
	got, err := loader.FileSHA256(path)
	if err != nil {
		return err
	}
	if got != m.DBCopy.SHA256 {
		return fmt.Errorf("%w: runner: the database copy's SHA-256 %.12s differs from the manifest's %.12s: it is not the copy the manifest was frozen for", ErrRefused, got, m.DBCopy.SHA256)
	}
	return nil
}

func newRun(prev manifest.Manifest, o RunOptions) (manifest.Manifest, error) {
	now := o.Now()
	next, err := manifest.NewRun(prev, o.NewRunReason, now, func(n *manifest.Manifest) {
		n.CodeVersion = CodeVersion()
		mm := o.Minimums
		if mm.MinDays != nil {
			n.Minimums.MinDays = mm.MinDays
		}
		if mm.MinCount != nil {
			n.Minimums.MinCount = mm.MinCount
		}
		if mm.DeltaLabelBps != nil {
			n.Minimums.DeltaLabelBps = mm.DeltaLabelBps
		}
		if mm.DeltaReplayDailyReturn != nil {
			n.Minimums.DeltaReplayDailyReturn = mm.DeltaReplayDailyReturn
		}
		if mm.Power != nil {
			n.Minimums.Power = mm.Power
		}
	})
	if err != nil {
		return manifest.Manifest{}, err
	}
	if err := next.Freeze(now); err != nil {
		return manifest.Manifest{}, err
	}
	return next, nil
}

func writePreRegistration(out string, m manifest.Manifest) (string, error) {
	if err := os.MkdirAll(out, 0o750); err != nil {
		return "", fmt.Errorf("runner: create %s: %w", out, err)
	}
	path := filepath.Join(out, PreRegistrationName(m.Run.Count))
	return path, manifest.WriteNew(path, m)
}

// target is the model the manifest froze, as stored in the copy.
type target struct {
	model *metalabel.Model
	// parseErr is why the stored parameters cannot score (model is nil).
	parseErr error
}

// loadTarget reads the model the manifest names BY VERSION (never "the latest
// rejected": a model stored after init is not picked up) and verifies its
// stored parameters against the frozen hash and its cohort against the
// manifest's. An unreadable parameter string is not an error here: the layers
// are reported as not executable.
func loadTarget(ctx context.Context, src *loader.Source, m manifest.Manifest) (target, error) {
	row, ok, err := src.ModelByVersion(ctx, m.Model.Version)
	if err != nil {
		return target{}, err
	}
	if !ok {
		return target{}, fmt.Errorf("%w: runner: the frozen model %q is not in the database copy", ErrRefused, m.Model.Version)
	}
	if err := m.CheckModel(row.Version, manifest.ParamsHash(row.ParamsJSON)); err != nil {
		return target{}, err
	}
	if c := row.Adoption.TrainedCohort; c != "" && c != m.Cohort {
		return target{}, fmt.Errorf("%w: runner: model %q was trained on cohort %q, the manifest freezes %q", ErrRefused, row.Version, c, m.Cohort)
	}
	model, perr := loader.ParseStored(row)
	return target{model: model, parseErr: perr}, nil
}

// evaluate runs both layers and builds the report document.
func evaluate(ctx context.Context, m manifest.Manifest, src *loader.Source) (report.Document, error) {
	t, err := loadTarget(ctx, src, m)
	if err != nil {
		return report.Document{}, err
	}
	var in report.Input
	in.Manifest = m
	if cv := CodeVersion(); cv != m.CodeVersion {
		in.Notes = append(in.Notes, fmt.Sprintf("コードの版が manifest と違う（manifest %s、この実行 %s）。同じコードで再現できるとは限らない", m.CodeVersion, cv))
	}
	if t.parseErr != nil {
		in.Label = report.LabelSection{Reason: notExecutable(t.parseErr)}
		in.Replay = report.ReplaySection{Reason: notExecutable(t.parseErr), UnavailablePeriods: []report.UnavailablePeriod{}}
		return report.Build(in)
	}
	// The original OOS rows (reference) are the earliest rows read; nothing
	// before oos_start is independent, so nothing before it is loaded.
	dec, err := src.LoadDecisions(ctx, m.Windows.OOSStart, m.Windows.EvalTo, m.Windows.OOSEnd)
	if err != nil {
		return report.Document{}, err
	}
	if in.Label, err = runLabel(ctx, m, t.model, dec); err != nil {
		return report.Document{}, err
	}
	if in.Replay, err = runReplay(ctx, m, src, t.model, dec); err != nil {
		return report.Document{}, err
	}
	return report.Build(in)
}
