package training

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// EvaluateForward is the forward (holdout) evaluation of an adopted model
// (issue #780): its taken signals' net return on the decisions made after the
// model was trained (rows decided at or after from, with a known label), which
// no adoption decision ever saw. It is reported next to the adoption record and
// never feeds the adoption criteria.
func EvaluateForward(m metalabel.Model, rows []Row, from, now time.Time, decide metalabel.DecideConfig, c AcceptanceConfig, seed uint64) repo.Forward {
	var window []Row
	for _, r := range rows {
		if !r.DecidedAt.Before(from) && !r.LabelEnd.After(now) {
			window = append(window, r)
		}
	}
	e := Evaluate(m, window, decide)
	f := repo.Forward{
		EvaluatedAt: now.UTC().Format(time.RFC3339), From: from.UTC().Format(time.RFC3339),
		Samples: e.Samples, Takes: e.Takes, EVBps: e.EVBps,
	}
	iv := e.Intervals(c, seed)
	f.Days, f.EVCI = iv.Days, iv.EV
	f.Degraded = iv.EV != nil && iv.EV.High < 0
	return f
}

// evaluateForward stores the forward evaluation of the model in force. A failure
// is logged and never fails the retraining: the forward evaluation is
// monitoring, separate from the adoption criteria.
func (t *Trainer) evaluateForward(ctx context.Context) {
	now := t.now().UTC()
	active, found, err := t.store.LatestActiveModel(ctx)
	if err != nil || !found {
		if err != nil {
			slog.WarnContext(ctx, "metalabel: forward evaluation skipped: load the active model", "error", err)
		}
		return
	}
	model, err := metalabel.ParseModel(active)
	if err != nil {
		slog.WarnContext(ctx, "metalabel: forward evaluation skipped: the active model is unusable", "version", active.Version, "error", err)
		return
	}
	settings, err := t.settings(ctx)
	if err != nil {
		slog.WarnContext(ctx, "metalabel: forward evaluation skipped: load settings", "error", err)
		return
	}
	stored, err := t.source.ListTrainingRows(ctx, active.TrainedAt, now)
	if err != nil {
		slog.WarnContext(ctx, "metalabel: forward evaluation skipped: load decisions", "error", err)
		return
	}
	rows, _ := BuildRows(stored)
	decide := metalabel.DecideConfig{MinEVBps: settings.MinEVBps, KellyFraction: metalabel.DefaultKellyFraction}
	f := EvaluateForward(model, rows, active.TrainedAt, now, decide, t.acceptance, uint64(now.UnixNano()))
	if err := t.store.UpdateForward(ctx, active.ID, f); err != nil {
		slog.WarnContext(ctx, "metalabel: store the forward evaluation failed", "version", active.Version, "error", err)
		return
	}
	level := slog.LevelInfo
	if f.Degraded {
		level = slog.LevelWarn
	}
	slog.Log(ctx, level, "metalabel: forward evaluation of the active model (holdout, not used for adoption)",
		"version", active.Version, "from", f.From, "samples", f.Samples, "takes", f.Takes, "days", f.Days,
		"ev_bps", derefFloat(f.EVBps), "ev_ci", formatInterval(f.EVCI), "degraded", f.Degraded)
}

func formatInterval(iv *repo.Interval) string {
	if iv == nil {
		return "undefined"
	}
	return fmt.Sprintf("[%.2f, %.2f]", iv.Low, iv.High)
}
