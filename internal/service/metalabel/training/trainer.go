package training

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/changeguard"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// DefaultLookback is how far back the nightly training reads labeled decisions.
const DefaultLookback = 90 * 24 * time.Hour

// TrainingSource lists the labeled decisions to learn from;
// *repo.Repository implements it.
type TrainingSource interface {
	ListTrainingRows(ctx context.Context, from, to time.Time) ([]repo.TrainingRow, error)
}

// ModelStore is the model storage the Trainer needs; *metalabel.Store
// implements it: saving a trained model, the model currently in force (the
// one a candidate is compared with and whose last adoption rate limits the
// next) and the forward evaluation of that model.
type ModelStore interface {
	SaveModel(ctx context.Context, m repo.Model) (repo.Model, error)
	LatestActiveModel(ctx context.Context) (repo.Model, bool, error)
	UpdateForward(ctx context.Context, id int64, f repo.Forward) error
}

// PolicyChangeSource tells when the self-improvement flow last changed the
// policy.* thresholds (FR-SELFIMPROVE-10); *judgement.ProposalRepository
// implements it.
type PolicyChangeSource interface {
	LastChangeAt(ctx context.Context) (time.Time, bool, error)
}

// SettingsLoader returns the effective meta-labeling settings (Settings
// screen values over the defaults); metalabelsettings.Load bound to the
// runtime settings store is one.
type SettingsLoader func(ctx context.Context) (metalabelsettings.Settings, error)

// Trainer is the nightly walk-forward retraining job of the secondary model:
// it reads the triple-barrier-labeled decisions, splits them by time with
// purge and embargo, learns and calibrates a model, evaluates it out of sample
// (on decisions after the LLM cutoff only) and stores it with its version and
// OOS metrics. Anything that goes wrong leaves the stored models untouched, so
// the previous model - or no model, which is a skip - stays in force.
type Trainer struct {
	source     TrainingSource
	store      ModelStore
	settings   SettingsLoader
	walk       WalkForwardConfig
	acceptance AcceptanceConfig
	lookback   time.Duration
	now        func() time.Time
	mode       metalabelsettings.Mode
	// policyChanges and limits rate limit the adoption against the
	// self-improvement flow's changes (issue #780); without a source only the
	// model-swap rate limit applies.
	policyChanges PolicyChangeSource
	limits        changeguard.Limits
}

// TrainerOption configures a Trainer.
type TrainerOption func(*Trainer)

// WithWalkForward overrides DefaultWalkForwardConfig.
func WithWalkForward(cfg WalkForwardConfig) TrainerOption { return func(t *Trainer) { t.walk = cfg } }

// WithAcceptance overrides DefaultAcceptanceConfig.
func WithAcceptance(cfg AcceptanceConfig) TrainerOption {
	return func(t *Trainer) { t.acceptance = cfg }
}

// WithLookback overrides DefaultLookback.
func WithLookback(d time.Duration) TrainerOption { return func(t *Trainer) { t.lookback = d } }

// WithMode sets the operating mode the adoption is judged for; the default is
// metalabelsettings.ModePaper, where an unverified LLM cutoff only warns.
func WithMode(m metalabelsettings.Mode) TrainerOption { return func(t *Trainer) { t.mode = m } }

// WithPolicyChanges makes the adoption honour the freeze after a policy.* change
// the self-improvement flow applied (issue #780).
func WithPolicyChanges(src PolicyChangeSource) TrainerOption {
	return func(t *Trainer) { t.policyChanges = src }
}

// WithChangeLimits overrides changeguard.DefaultLimits.
func WithChangeLimits(l changeguard.Limits) TrainerOption { return func(t *Trainer) { t.limits = l } }

// WithClock overrides time.Now (tests).
func WithClock(now func() time.Time) TrainerOption { return func(t *Trainer) { t.now = now } }

// NewTrainer returns a Trainer reading from source and storing through store,
// with the settings settings returns at each run.
func NewTrainer(source TrainingSource, store ModelStore, settings SettingsLoader, opts ...TrainerOption) *Trainer {
	t := &Trainer{
		source: source, store: store, settings: settings,
		walk: DefaultWalkForwardConfig(), acceptance: DefaultAcceptanceConfig(), lookback: DefaultLookback, now: time.Now,
		limits: changeguard.DefaultLimits(), mode: metalabelsettings.ModePaper,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Retrain is the nightly job's entry: it trains and stores one model. Too little
// labeled history to learn from (ErrInsufficientData) is logged and is not an
// error: it is the normal state until paper trading has produced labels, and
// nothing is stored. Every other failure is returned and leaves the stored
// models untouched.
func (t *Trainer) Retrain(ctx context.Context) error {
	_, err := t.Train(ctx)
	// The model in force is monitored on decisions made since it was trained
	// whether or not tonight's training produced anything.
	t.evaluateForward(ctx)
	if errors.Is(err, ErrInsufficientData) {
		slog.Warn("metalabel: nightly retraining skipped; the existing model (or a skip) stays in force", "reason", err)
		return nil
	}
	return err
}

// Train runs one walk-forward training as of now and stores the resulting
// model: active when its OOS metrics clear the acceptance bar, otherwise
// rejected with the reason (kept for the record only). It returns the stored
// model. It fails with ErrInsufficientData (nothing stored) when the history
// is too thin or one-sided to learn from.
func (t *Trainer) Train(ctx context.Context) (repo.Model, error) {
	settings, err := t.settings(ctx)
	if err != nil {
		return repo.Model{}, fmt.Errorf("metalabel: load settings: %w", err)
	}
	if settings.Model != metalabelsettings.ModelLogisticRegression {
		return repo.Model{}, fmt.Errorf("metalabel: unsupported model %q", settings.Model)
	}
	if settings.Calibration != metalabel.CalibrationPlatt && settings.Calibration != metalabel.CalibrationIsotonic {
		return repo.Model{}, fmt.Errorf("metalabel: unsupported calibration %q", settings.Calibration)
	}
	cutoff, err := settings.LLMCutoffTime()
	if err != nil {
		return repo.Model{}, err
	}
	logCutoffWarning(ctx, settings)
	now := t.now().UTC()

	stored, err := t.source.ListTrainingRows(ctx, now.Add(-t.lookback), now)
	if err != nil {
		return repo.Model{}, fmt.Errorf("metalabel: load training rows: %w", err)
	}
	rows, dropped := BuildRows(stored)
	split, err := SplitWalkForward(rows, now, cutoff, t.walk)
	if err != nil {
		return repo.Model{}, err
	}
	// The Jev-feature evaluation window is logged on every run so the
	// operator can see it never starts before the LLM's cutoff.
	slog.Info("metalabel: walk-forward windows",
		"llm_cutoff", cutoff.Format(metalabelsettings.LLMCutoffLayout),
		"train_start", split.TrainStart, "train_end", split.TrainEnd,
		"oos_start", split.OOSStart, "oos_end", split.OOSEnd,
		"oos_after_llm_cutoff", !split.OOSStart.Before(cutoff),
		"fit_samples", len(split.Fit), "calibration_samples", len(split.Calibrate), "oos_samples", len(split.OOS),
		"purged", split.Purged, "embargo", t.walk.Embargo, "dropped_incomplete", dropped, "fit_before_llm_cutoff", fitBeforeCutoff(split))

	version := "lr-" + now.Format("20060102-150405")
	model, err := metalabel.Train(samples(split.Fit), samples(split.Calibrate), metalabel.TrainOptions{
		Version: version, Calibration: settings.Calibration, LR: metalabel.DefaultTrainConfig(),
	})
	if err != nil {
		if errors.Is(err, metalabel.ErrInvalidTrainingData) || errors.Is(err, metalabel.ErrInvalidCalibration) {
			return repo.Model{}, fmt.Errorf("%w: %w", ErrInsufficientData, err)
		}
		return repo.Model{}, fmt.Errorf("metalabel: train %s: %w", version, err)
	}

	decide := metalabel.DecideConfig{MinEVBps: settings.MinEVBps, KellyFraction: metalabel.DefaultKellyFraction}
	eval := Evaluate(model, split.OOS, decide)
	verdict, err := t.judge(ctx, now, settings, split, eval)
	if err != nil {
		return repo.Model{}, err
	}

	params, err := model.ParamsJSON()
	if err != nil {
		return repo.Model{}, err
	}
	saved, err := t.store.SaveModel(ctx, repo.Model{
		Version: version, Algorithm: metalabel.AlgorithmLogisticRegression, LabelVersion: domain.CalibrationLabelVersionTripleBarrier,
		Status: verdict.status, RejectReason: verdict.reason, TrainedAt: now,
		TrainStart: split.TrainStart, TrainEnd: split.TrainEnd, OOSStart: split.OOSStart, OOSEnd: split.OOSEnd,
		TrainSamples: len(split.Fit) + len(split.Calibrate), OOSSamples: eval.Samples,
		FeatureNames: model.FeatureNames, ParamsJSON: params,
		OOSAUC: eval.AUC, OOSExpectedValueBps: eval.EVBps, OOSCalibration: eval.Calibration,
		OOSTakes: eval.Takes, OOSDays: verdict.intervals.Days, OOSEVCI: verdict.intervals.EV, OOSAUCCI: verdict.intervals.AUC,
		Adoption: verdict.adoption,
	})
	if err != nil {
		return repo.Model{}, fmt.Errorf("metalabel: save model %s: %w", version, err)
	}
	slog.Info("metalabel: model trained", "version", version, "status", verdict.status, "reject_reason", verdict.reason,
		"oos_samples", eval.Samples, "oos_takes", eval.Takes, "oos_days", verdict.intervals.Days,
		"oos_auc", derefFloat(eval.AUC), "oos_auc_ci", formatInterval(verdict.intervals.AUC),
		"oos_ev_bps", derefFloat(eval.EVBps), "oos_ev_ci", formatInterval(verdict.intervals.EV),
		"min_oos_samples", t.acceptance.MinOOSSamples, "min_takes", t.acceptance.MinTakes, "min_oos_days", t.acceptance.MinOOSDays)
	return saved, nil
}

func samples(rows []Row) []metalabel.Sample {
	out := make([]metalabel.Sample, len(rows))
	for i, r := range rows {
		out[i] = r.Sample()
	}
	return out
}

// derefFloat renders an optional metric for the log: NaN when undefined.
func derefFloat(f *float64) float64 {
	if f == nil {
		return math.NaN()
	}
	return *f
}
