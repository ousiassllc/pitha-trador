package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/loader"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/replaylayer"
)

// Statistics defaults, as proposed in docs/research/metaablation-thresholds.md
// (a proposal, not a decision): the pre-registered family is the four primary
// comparisons of each of the two layers.
const (
	DefaultAlpha     = 0.05
	DefaultResamples = 10000
	DefaultSeed      = 1046
)

// InitOptions are the inputs of `init`.
type InitOptions struct {
	DBPath, OutDir string
	// AsOf is the time the database copy reflects; EvalFrom and EvalTo bound
	// the evaluation window [EvalFrom, EvalTo).
	AsOf, EvalFrom, EvalTo time.Time
	// Cohort overrides the cohort recorded with the model (required when the
	// model row has none); it must equal the stored one when both exist.
	Cohort  string
	Seed    int64
	Embargo time.Duration
	// Supersedes is an earlier frozen pre-registration of the same study (same
	// database copy and model) that this one replaces; NewRunReason says why.
	// Both are required when out already holds a pre-registration of the same
	// copy and model: a changed rule value is a new run, never a fresh run 1.
	Supersedes, NewRunReason string
	// ModelVersion names the rejected model to pin; "" pins the latest rejected
	// model at init time. Either way the manifest records the version and how
	// it was chosen, and `run` reads that version whatever is stored later.
	ModelVersion string
	Rules        RuleOverrides
	// Minimums are the pre-registered thresholds of the verdict; nil stays
	// unset (every verdict is then 優劣未確定).
	Minimums manifest.Minimums
	// Now is the clock (time.Now when nil).
	Now func() time.Time
}

// Init pins the model under test (the latest rejected one, or the one named by
// ModelVersion) and pre-registers the run: it reads the
// model row, the model's own windows and settings, never a decision, label or
// snapshot of the evaluation window, and writes the frozen manifest into
// OutDir. The database copy is hashed before and after and must not change.
func Init(ctx context.Context, o InitOptions) (manifestPath string, m manifest.Manifest, err error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	out, err := checkOutDir(o.OutDir, o.DBPath)
	if err != nil {
		return "", manifest.Manifest{}, err
	}
	if !o.EvalFrom.Before(o.EvalTo) {
		return "", manifest.Manifest{}, errors.New("runner: -from must be before -to")
	}
	if o.AsOf.IsZero() {
		return "", manifest.Manifest{}, errors.New("runner: -asof is required (the time the database copy reflects)")
	}
	if o.EvalTo.After(o.AsOf) {
		return "", manifest.Manifest{}, fmt.Errorf("runner: -to %s is after -asof %s: the copy cannot hold the whole evaluation window", o.EvalTo.Format(time.RFC3339), o.AsOf.Format(time.RFC3339))
	}
	if o.Embargo != 0 && o.Embargo < loader.DefaultEmbargo {
		return "", manifest.Manifest{}, fmt.Errorf("runner: -embargo %s is below the walk-forward default %s: a shorter embargo would loosen the independence boundary", o.Embargo, loader.DefaultEmbargo)
	}
	if err := loader.CheckCheckpointed(o.DBPath); err != nil {
		return "", manifest.Manifest{}, fmt.Errorf("%w: %w", ErrRefused, err)
	}
	before, err := loader.FileSHA256(o.DBPath)
	if err != nil {
		return "", manifest.Manifest{}, err
	}
	src, err := loader.Open(o.DBPath)
	if err != nil {
		return "", manifest.Manifest{}, err
	}
	defer func() { _ = src.Close() }()

	model, selection, err := pinModel(ctx, src, o.ModelVersion)
	if err != nil {
		return "", manifest.Manifest{}, err
	}
	if o.EvalFrom.Before(model.OOSEnd) {
		return "", manifest.Manifest{}, fmt.Errorf("runner: -from %s is before the model's original OOS end %s: init would read labels of the model's own windows as evaluation data; start the evaluation window at or after it", o.EvalFrom.Format(time.RFC3339), model.OOSEnd.Format(time.RFC3339))
	}
	cohort, err := resolveCohort(model.Adoption.TrainedCohort, o.Cohort)
	if err != nil {
		return "", manifest.Manifest{}, fmt.Errorf("runner: model %s: %w", model.Version, err)
	}
	embargo := o.Embargo
	if embargo == 0 {
		embargo = loader.DefaultEmbargo
	}
	w, err := src.ModelWindows(ctx, model, embargo)
	if err != nil {
		return "", manifest.Manifest{}, err
	}
	w.EvalFrom, w.EvalTo = o.EvalFrom, o.EvalTo
	rules, err := DefaultRules(o.Rules)
	if err != nil {
		return "", manifest.Manifest{}, err
	}
	notes, err := replayNotes(rules)
	if err != nil {
		return "", manifest.Manifest{}, err
	}
	notes = append(notes, w.Notes...)
	notes = append(notes, "rule values come from the shipped defaults and the init flags; no decision, label or snapshot of the evaluation window was read before this manifest was frozen")
	if _, err := loader.ParseStored(model); err != nil {
		notes = append(notes, "the stored model parameters were not readable at init; run will report the layers as not executable: "+err.Error())
	}
	now := o.Now()
	m = manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion, CreatedAt: now,
		Model:       manifest.ModelRef{Version: model.Version, ParamsSHA256: manifest.ParamsHash(model.ParamsJSON), Selection: selection},
		DBCopy:      manifest.DBCopyRef{SHA256: before, AsOf: o.AsOf},
		CodeVersion: CodeVersion(), Cohort: cohort, Windows: w.Windows, Rules: rules,
		Statistics: statisticsPlan(o.Seed), Minimums: o.Minimums, Exclusions: []manifest.Exclusion{},
		Replay:     manifest.ReplayPlan{InitialState: manifest.InitialState{CapitalJPY: rules.Replay.InitialCapitalJPY}, SegmentRule: replaylayer.SegmentPolicy},
		Provenance: manifest.Provenance{ChosenBeforeEvaluation: true, Notes: notes},
		Run:        manifest.Run{Count: 1},
	}
	if m.Run, err = chainRun(out, o, m, now); err != nil {
		return "", manifest.Manifest{}, err
	}
	if err := m.Freeze(now); err != nil {
		return "", manifest.Manifest{}, err
	}
	after, err := loader.FileSHA256(o.DBPath)
	if err != nil {
		return "", manifest.Manifest{}, err
	}
	if after != before {
		return "", manifest.Manifest{}, errors.New("runner: the database copy changed while init was reading it")
	}
	path := filepath.Join(out, PreRegistrationName(m.Run.Count))
	if err := os.MkdirAll(out, 0o750); err != nil {
		return "", manifest.Manifest{}, fmt.Errorf("runner: create %s: %w", out, err)
	}
	if err := manifest.WriteNew(path, m); err != nil {
		return "", manifest.Manifest{}, err
	}
	return path, m, nil
}

// pinModel is the model init freezes and how it was chosen: the stored model
// named by version (SelectionExplicit) or, with no version, the latest
// rejected one (SelectionLatestRejected). An explicit version must exist, must
// be a rejected model and must keep parameters that can score: the study is
// never silently moved to another model.
func pinModel(ctx context.Context, src *loader.Source, version string) (repo.Model, string, error) {
	if version == "" {
		model, ok, err := src.LatestRejected(ctx)
		if err != nil {
			return repo.Model{}, "", err
		}
		if !ok {
			return repo.Model{}, "", errors.New("runner: the database copy holds no rejected model to re-score")
		}
		return model, manifest.SelectionLatestRejected, nil
	}
	model, ok, err := src.ModelByVersion(ctx, version)
	if err != nil {
		return repo.Model{}, "", err
	}
	if !ok {
		return repo.Model{}, "", fmt.Errorf("%w: runner: -model %q is not in the database copy", ErrRefused, version)
	}
	if model.Status != repo.ModelRejected {
		return repo.Model{}, "", fmt.Errorf("%w: runner: -model %q is %s, not a rejected model: only a rejected model is re-scored", ErrRefused, version, model.Status)
	}
	if _, err := loader.ParseStored(model); err != nil {
		return repo.Model{}, "", fmt.Errorf("%w: runner: -model %q cannot be re-scored, its stored parameters are not readable: %w", ErrRefused, version, err)
	}
	return model, manifest.SelectionExplicit, nil
}

// ErrRefused marks an input the command refuses on purpose (a changed or foreign
// manifest or copy, a missing supersede, an output inside the copy): not a usage
// mistake and not an I/O failure.
var ErrRefused = errors.New("refused")

// PreRegistrationName is the file name of run n's frozen manifest.
func PreRegistrationName(n int) string {
	return fmt.Sprintf("metaablation-run%d.prereg.manifest.json", n)
}

// resolveCohort takes the model's recorded cohort and the explicit one: an
// explicit cohort may fill a missing record but never contradict it. The cohort
// is never guessed.
func resolveCohort(stored, explicit string) (string, error) {
	switch {
	case stored != "" && explicit != "" && stored != explicit:
		return "", fmt.Errorf("-cohort %q contradicts the cohort %q recorded with the model", explicit, stored)
	case stored != "":
		return stored, nil
	case explicit != "":
		return explicit, nil
	}
	return "", errors.New("no trained cohort is recorded with the model (it was stored before issue #881); pass -cohort explicitly: the evaluation never runs without a cohort check")
}

// statisticsPlan is the pre-registered statistics plan: both layers' primary
// metrics, the four primary one-axis comparisons per layer, Bonferroni.
func statisticsPlan(seed int64) manifest.Statistics {
	var family []string
	for _, c := range metaablation.Comparisons() {
		if c.Primary {
			family = append(family, c.Name)
		}
	}
	if seed == 0 {
		seed = DefaultSeed
	}
	return manifest.Statistics{
		Method: manifest.TestMethod,
		PrimaryMetrics: []manifest.LayerMetric{
			{Layer: manifest.LayerLabel, Metric: "net_return_bps_per_eligible_candidate"},
			{Layer: manifest.LayerReplay, Metric: "daily_return_on_capital"},
		},
		ComparisonFamily: family, NumComparisons: 2 * len(family), Correction: manifest.CorrectionBonferroni,
		Alpha: DefaultAlpha, Resamples: DefaultResamples, Seed: seed,
	}
}
