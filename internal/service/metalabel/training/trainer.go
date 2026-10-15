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
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// Reasons recorded in metalabel_models.reject_reason for a model that learned
// but did not meet the acceptance bar.
const (
	RejectOOSSamples = "oos_samples_below_minimum"
	RejectAUC        = "oos_auc_not_above_minimum"
	RejectTakes      = "oos_takes_below_minimum"
	RejectEV         = "oos_expected_value_not_positive"
)

// AcceptanceConfig is the OOS bar a freshly trained model must clear to be
// adopted. A model below it is still stored (status rejected) so its metrics
// stay on record, but LatestActiveModel never returns it, so the previous
// model (or, with none, a skip) stays in force.
type AcceptanceConfig struct {
	// MinOOSSamples is the fewest scored OOS rows an evaluation is trusted on.
	MinOOSSamples int
	// MinAUC is the OOS AUC the model must exceed (0.5 is no better than chance).
	MinAUC float64
	// MinTakes is the fewest OOS signals the model must have taken for their
	// mean EV to mean anything; that EV must also be positive.
	MinTakes int
}

// DefaultAcceptanceConfig returns 30 OOS samples, an AUC above 0.5 and at
// least 10 taken signals with a positive mean EV.
func DefaultAcceptanceConfig() AcceptanceConfig {
	return AcceptanceConfig{MinOOSSamples: 30, MinAUC: 0.5, MinTakes: 10}
}

// Judge returns repo.ModelActive with an empty reason when e clears the bar,
// otherwise repo.ModelRejected with why.
func (c AcceptanceConfig) Judge(e Evaluation) (status, reason string) {
	switch {
	case e.Samples < c.MinOOSSamples:
		return repo.ModelRejected, RejectOOSSamples
	case e.AUC == nil || !(*e.AUC > c.MinAUC):
		return repo.ModelRejected, RejectAUC
	case e.Takes < c.MinTakes:
		return repo.ModelRejected, RejectTakes
	case e.EVBps == nil || !(*e.EVBps > 0):
		return repo.ModelRejected, RejectEV
	}
	return repo.ModelActive, ""
}

// DefaultLookback is how far back the nightly training reads labeled decisions.
const DefaultLookback = 90 * 24 * time.Hour

// TrainingSource lists the labeled decisions to learn from;
// *repo.Repository implements it.
type TrainingSource interface {
	ListTrainingRows(ctx context.Context, from, to time.Time) ([]repo.TrainingRow, error)
}

// ModelSaver stores a trained model; *Store implements it.
type ModelSaver interface {
	SaveModel(ctx context.Context, m repo.Model) (repo.Model, error)
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
	saver      ModelSaver
	settings   SettingsLoader
	walk       WalkForwardConfig
	acceptance AcceptanceConfig
	lookback   time.Duration
	now        func() time.Time
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

// WithClock overrides time.Now (tests).
func WithClock(now func() time.Time) TrainerOption { return func(t *Trainer) { t.now = now } }

// NewTrainer returns a Trainer reading from source and storing through saver,
// with the settings settings returns at each run.
func NewTrainer(source TrainingSource, saver ModelSaver, settings SettingsLoader, opts ...TrainerOption) *Trainer {
	t := &Trainer{
		source: source, saver: saver, settings: settings,
		walk: DefaultWalkForwardConfig(), acceptance: DefaultAcceptanceConfig(), lookback: DefaultLookback, now: time.Now,
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
		"purged", split.Purged, "embargo", t.walk.Embargo, "dropped_incomplete", dropped)

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

	eval := Evaluate(model, split.OOS, metalabel.DecideConfig{MinEVBps: settings.MinEVBps, KellyFraction: metalabel.DefaultKellyFraction})
	status, reason := t.acceptance.Judge(eval)

	params, err := model.ParamsJSON()
	if err != nil {
		return repo.Model{}, err
	}
	saved, err := t.saver.SaveModel(ctx, repo.Model{
		Version: version, Algorithm: metalabel.AlgorithmLogisticRegression, LabelVersion: domain.CalibrationLabelVersionTripleBarrier,
		Status: status, RejectReason: reason, TrainedAt: now,
		TrainStart: split.TrainStart, TrainEnd: split.TrainEnd, OOSStart: split.OOSStart, OOSEnd: split.OOSEnd,
		TrainSamples: len(split.Fit) + len(split.Calibrate), OOSSamples: eval.Samples,
		FeatureNames: model.FeatureNames, ParamsJSON: params,
		OOSAUC: eval.AUC, OOSExpectedValueBps: eval.EVBps, OOSCalibration: eval.Calibration,
	})
	if err != nil {
		return repo.Model{}, fmt.Errorf("metalabel: save model %s: %w", version, err)
	}
	slog.Info("metalabel: model trained", "version", version, "status", status, "reject_reason", reason,
		"oos_samples", eval.Samples, "oos_auc", derefFloat(eval.AUC), "oos_takes", eval.Takes, "oos_ev_bps", derefFloat(eval.EVBps))
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
