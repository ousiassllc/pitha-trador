package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/independence"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/labellayer"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/loader"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/replaylayer"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/report"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// notExecutable is the reason shown for both layers when the model's stored
// parameters cannot be parsed (the body is never printed).
func notExecutable(err error) string {
	return "実行不可: 保存されたモデルのパラメータを読めず再採点できない（" + oneLine(err) + "）"
}

func oneLine(err error) string { return strings.Join(strings.Fields(err.Error()), " ") }

// runLabel runs table 1 on the loaded decisions. A model that cannot score
// makes the layer not executable, with the reason; any other failure is an
// error.
func runLabel(ctx context.Context, m manifest.Manifest, model *metalabel.Model, dec loader.Decisions) (report.LabelSection, error) {
	res, err := labellayer.Run(ctx, labellayer.Input{
		Manifest: m, Model: model, ParamsSHA256: m.Model.ParamsSHA256, OOSUsed: dec.OOSUsed, Rows: dec.Rows,
	})
	switch {
	case errors.Is(err, labellayer.ErrNotRescorable):
		return report.LabelSection{Reason: "実行不可: " + oneLine(err)}, nil
	case err != nil:
		return report.LabelSection{}, err
	}
	return report.LabelSection{Available: true, Result: &res}, nil
}

// boundary is the instant after which decisions are independent (the latest of
// the train, calibration and original OOS ends and LabelEnd + embargo).
func boundary(w manifest.Windows) time.Time {
	b := w.TrainEnd
	for _, t := range []time.Time{w.CalibrationEnd, w.OOSEnd, w.LabelEnd.Add(w.Embargo())} {
		if t.After(b) {
			b = t
		}
	}
	return b
}

// runReplay runs table 2: the independent decisions of the evaluation window
// replayed continuously in the four cells over the market_snapshots of the
// copy. Whatever keeps the layer from running (no data, a changed code default,
// an engine refusal) is its reason; the label layer is not affected.
func runReplay(ctx context.Context, m manifest.Manifest, src *loader.Source, model *metalabel.Model, dec loader.Decisions) (report.ReplaySection, error) {
	unavailable := func(format string, args ...any) (report.ReplaySection, error) {
		return report.ReplaySection{Reason: "実行不可: " + fmt.Sprintf(format, args...), UnavailablePeriods: []report.UnavailablePeriod{}}, nil
	}
	cfg, err := newReplayConfig(m.Rules)
	if err != nil {
		return unavailable("%s", oneLine(err))
	}
	fp, err := cfg.fingerprint()
	if err != nil {
		return report.ReplaySection{}, err
	}
	if !slicesContains(m.Provenance.Notes, fingerprintNote+fp) {
		return unavailable("再生の設定（Risk・出口・費用・ガード）が manifest に固定した指紋と違う。コードの既定値が変わったか、manifest に指紋が無い。新しい実行として固定し直す")
	}
	ic := independence.Config{Windows: m.Windows, Cohort: m.Cohort}
	var independent []domain.JevDecision
	for _, d := range dec.All {
		if ic.Classify(independence.Row{ID: d.ID, DecidedAt: d.Timestamp, Cohort: loader.Cohort(d)}).Class == independence.Independent {
			independent = append(independent, d)
		}
	}
	from, to := m.Windows.EvalFrom, m.Windows.EvalTo
	if b := boundary(m.Windows); b.After(from) {
		from = b
	}
	if len(independent) == 0 || !from.Before(to) {
		return unavailable("評価窓 [%s, %s) に独立な判断が無い", from.In(marketcalendar.JST).Format(time.RFC3339), to.In(marketcalendar.JST).Format(time.RFC3339))
	}
	mk, err := src.LoadMarket(ctx, independent, from, to)
	if err != nil {
		return report.ReplaySection{}, err
	}
	if len(mk.Instruments) == 0 {
		return unavailable("評価窓の market_snapshots が写しに無い（独立な判断 %d 件の銘柄に足がない）", len(independent))
	}
	res, err := replaylayer.Run(ctx, replaylayer.Input{
		From: from, To: to, Instruments: mk.Instruments, Decisions: independent,
		Thresholds: cfg.Thresholds, Risk: cfg.Risk, Cost: cfg.Cost, Exit: cfg.Exit, Sessions: marketcalendar.TSE,
		Model: model, Meta: metalabel.DecideConfig{MinEVBps: m.Rules.MinEVBps, KellyFraction: metalabel.DefaultKellyFraction},
		Unavailable: mk.Unavailable,
	})
	if err != nil {
		if ctx.Err() != nil {
			return report.ReplaySection{}, ctx.Err()
		}
		return unavailable("%s", oneLine(err))
	}
	plan, err := stats.PlanFrom(m, manifest.LayerReplay)
	if err != nil {
		return report.ReplaySection{}, err
	}
	sec, err := toSection(res, plan, m)
	if err != nil {
		return report.ReplaySection{}, err
	}
	if len(sec.Segments) == 0 {
		sec.Available, sec.Reason = false, "実行不可: 再生できる区間が無い（すべての日の market_snapshots が欠けている）。期間は一覧のとおり"
		return sec, nil
	}
	return sec, nil
}

func slicesContains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
