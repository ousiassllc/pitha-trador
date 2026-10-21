// Package metalabeldiag holds the read-only logic behind the meta-labeling
// diagnosis tools metalabel_training_readiness and jev_models (issues #848,
// #849; docs/api/endpoints/paper-results-mcp-metalabel.md). It lives apart from
// package query, whose directory is at its size limit, and reads through the
// same query.Q.
package metalabeldiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// MaxReadinessRows is the most training rows metalabel_training_readiness
// reads (it rebuilds each row's features from its stored state, like the
// nightly retraining does). A period holding more is limit_exceeded: narrow it.
const MaxReadinessRows = 20000

// ReadinessStep is one condition of the training-row funnel and the decisions
// left after it (each step includes the ones before).
type ReadinessStep struct {
	Step      string `json:"step"`
	Condition string `json:"condition"`
	Rows      int64  `json:"rows"`
}

// The funnel's Step codes, in order.
const (
	StepTraderDecisions  = "trader_decisions"
	StepLongShort        = "long_short"
	StepLabelV2          = "label_v2"
	StepNetReturn        = "net_return"
	StepFeaturesComplete = "features_complete"
)

// FeatureGap is how many training rows lack one feature (the features of a
// row can be missing together, so the gaps overlap).
type FeatureGap struct {
	Feature     string `json:"feature"`
	MissingRows int    `json:"missing_rows"`
}

// FeatureGaps are the training rows BuildRows keeps and drops.
// DroppedIncomplete is what the retraining logs as dropped_incomplete.
type FeatureGaps struct {
	TrainingRows      int          `json:"training_rows"`
	UsableRows        int          `json:"usable_rows"`
	DroppedIncomplete int          `json:"dropped_incomplete"`
	Missing           []FeatureGap `json:"missing"`
}

// The values of LastRetrain.DroppedIncompleteStatus.
const (
	// DroppedRecorded: the latest skip stored dropped_incomplete.
	DroppedRecorded = "recorded"
	// DroppedNotInSchema: this database has no dropped_incomplete column on
	// metalabel_retrain_skips (a build before issue #862), so nothing stored it.
	DroppedNotInSchema = "not_recorded_in_schema"
	// DroppedNotOnRow: the column exists but the latest skip's value is NULL
	// (the column is nullable in this database), so that night stored none.
	DroppedNotOnRow = "not_recorded_on_row"
	// DroppedNoSkip: the latest retraining stored a model (or none ran), so
	// there is no skip to carry it. The live count is Features.DroppedIncomplete.
	DroppedNoSkip = "no_skip_recorded"
)

// LastRetrainSkip is the newest metalabel_retrain_skips row. DroppedIncomplete
// is null when the schema does not store it.
type LastRetrainSkip struct {
	SkippedAt         string `json:"skipped_at"`
	ReasonCode        string `json:"reason_code"`
	UsableRows        int64  `json:"usable_rows"`
	RequiredRows      int64  `json:"required_rows"`
	DroppedIncomplete *int64 `json:"dropped_incomplete"`
}

// LastRetrain is what the latest nightly retraining left behind: the newest
// skip and the newest stored model, and which is newer. LastRun is "skipped",
// "model_stored" or "none". Any period: this is the latest row, not a count.
type LastRetrain struct {
	LastRun                string           `json:"last_run"`
	LatestSkip             *LastRetrainSkip `json:"latest_skip"`
	LatestModelTrainedAt   *string          `json:"latest_model_trained_at"`
	DroppedIncompleteState string           `json:"dropped_incomplete_status"`
}

// TrainSide is the fit + calibration side of the walk-forward split. Rows is
// the count a skipped retraining records as usable_rows (rows before the OOS
// window that can be learned from) and Required the "必要130件" it is shown
// against; after a successful split Rows is fit + calibration. FitRows and
// CalibrationRows are null while the split did not complete.
type TrainSide struct {
	Rows                    int  `json:"rows"`
	RequiredRows            int  `json:"required_rows"`
	FitRows                 *int `json:"fit_rows"`
	CalibrationRows         *int `json:"calibration_rows"`
	RequiredFitRows         int  `json:"required_fit_rows"`
	RequiredCalibrationRows int  `json:"required_calibration_rows"`
}

// OOSSide is the out-of-sample side: the rows after the OOS window opens
// against the adoption bar (training.DefaultAcceptanceConfig). Ready is true
// when both the sample and the day minimums are met; the take minimum needs
// the model's own take rate and is not judged here.
type OOSSide struct {
	Rows         int  `json:"rows"`
	RequiredRows int  `json:"required_rows"`
	Days         int  `json:"days"`
	RequiredDays int  `json:"required_days"`
	RequiredTake int  `json:"required_takes"`
	Ready        bool `json:"ready"`
}

// ReadinessSplit is the walk-forward split the nightly retraining would make
// of the rows read, against today's llm_cutoff. TrainReady is true when the
// split completes; otherwise TrainSkipCode is the reason code the retraining
// would record (a skip code of internal/repository/metalabel).
type ReadinessSplit struct {
	LLMCutoff       string    `json:"llm_cutoff"`
	LLMCutoffStatus string    `json:"llm_cutoff_status"`
	OOSStart        string    `json:"oos_start"`
	TrainReady      bool      `json:"train_ready"`
	TrainSkipCode   *string   `json:"train_skip_code"`
	Train           TrainSide `json:"train"`
	OOS             OOSSide   `json:"oos"`
}

// TrainingReadiness is metalabel_training_readiness' data (issue #848).
// RowsFrom/RowsTo is the decision window actually read: the period cut to the
// retraining's lookback (LookbackDays) and to AsOf (the period end or now,
// whichever is first). MatchesNightlyRetrain is true when that window is the
// one tonight's retraining reads, so the counts are the ones it will see.
type TrainingReadiness struct {
	AsOf                  string          `json:"as_of"`
	RowsFrom              string          `json:"rows_from"`
	RowsTo                string          `json:"rows_to"`
	LookbackDays          int             `json:"lookback_days"`
	MatchesNightlyRetrain bool            `json:"matches_nightly_retrain"`
	Funnel                []ReadinessStep `json:"funnel"`
	Features              FeatureGaps     `json:"features"`
	LastRetrain           LastRetrain     `json:"last_retrain"`
	Split                 ReadinessSplit  `json:"split"`
}

// TrainingReadinessAt reports whether the labeled Jev decisions of r can feed
// the meta-labeling retraining. It runs the retraining's own code on the same
// rows, training.BuildRowsReport (BuildRows plus the missing features) for the
// features, training.SplitWalkForward with the
// default walk-forward and acceptance configs for the split and the required
// counts (so a changed default shows up here without an edit). Read-only.
func TrainingReadinessAt(ctx context.Context, q query.Q, r query.Range, now time.Time) (TrainingReadiness, error) {
	now = now.UTC()
	asOf := now
	if r.To.Before(asOf) {
		asOf = r.To.UTC()
	}
	lookback := training.DefaultLookback
	rowsFrom := asOf.Add(-lookback)
	if r.From.After(rowsFrom) {
		rowsFrom = r.From.UTC()
	}
	out := TrainingReadiness{
		AsOf: asOf.Format(time.RFC3339), RowsFrom: rowsFrom.Format(time.RFC3339), RowsTo: asOf.Format(time.RFC3339),
		LookbackDays:          int(lookback / (24 * time.Hour)),
		MatchesNightlyRetrain: asOf.Equal(now) && !r.From.After(now.Add(-lookback)),
	}
	if !rowsFrom.Before(asOf) {
		rowsFrom = asOf // an empty window
	}
	var err error
	if out.Funnel, err = readinessFunnel(ctx, q, rowsFrom, asOf); err != nil {
		return TrainingReadiness{}, err
	}
	if out.Funnel[3].Rows > MaxReadinessRows {
		return TrainingReadiness{}, query.ErrLimitExceeded
	}
	stored, err := repo.ListTrainingRowsFrom(ctx, q, rowsFrom, asOf)
	if err != nil {
		return TrainingReadiness{}, fmt.Errorf("query: training rows: %w", err)
	}
	rows, dropped, missing := training.BuildRowsReport(stored)
	out.Funnel = append(out.Funnel, ReadinessStep{Step: StepFeaturesComplete, Condition: "8 つの特徴量がそろう（BuildRows が残す行 = 学習に使える行）", Rows: int64(len(rows))})
	out.Features = featureGaps(len(stored), len(rows), dropped, missing)
	if out.LastRetrain, err = lastRetrain(ctx, q); err != nil {
		return TrainingReadiness{}, err
	}
	settings, err := loadMetalabelSettings(ctx, q)
	if err != nil {
		return TrainingReadiness{}, err
	}
	cutoff, err := settings.LLMCutoffTime()
	if err != nil {
		return TrainingReadiness{}, fmt.Errorf("query: %w", err)
	}
	if out.Split, err = readinessSplit(rows, asOf, cutoff); err != nil {
		return TrainingReadiness{}, err
	}
	out.Split.LLMCutoff, out.Split.LLMCutoffStatus = settings.LLMCutoff, settings.CutoffStatus()
	return out, nil
}

// readinessFunnel counts the trader decisions of [from, to) through the
// conditions of repo.ListTrainingRowsFrom, one statement.
func readinessFunnel(ctx context.Context, q query.Q, from, to time.Time) ([]ReadinessStep, error) {
	f, t := query.Range{From: from, To: to}.Bounds()
	v := domain.CalibrationLabelVersionTripleBarrier
	var traders, longShort, labeled, netReturn int64
	err := q.QueryRowContext(ctx, `
SELECT COUNT(*),
	COALESCE(SUM(d.direction IN ('LONG', 'SHORT')), 0),
	COALESCE(SUM(d.direction IN ('LONG', 'SHORT') AND EXISTS (
		SELECT 1 FROM calibration_outcomes o WHERE o.jev_decision_id = d.id AND o.label_version = ?)), 0),
	COALESCE(SUM(d.direction IN ('LONG', 'SHORT') AND EXISTS (
		SELECT 1 FROM calibration_outcomes o
		WHERE o.jev_decision_id = d.id AND o.label_version = ? AND o.net_return_bps IS NOT NULL)), 0)
FROM jev_decisions d
WHERE d.decision_type = 'trader' AND d.timestamp >= ? AND d.timestamp < ?`, v, v, f, t).
		Scan(&traders, &longShort, &labeled, &netReturn)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("query: training funnel: %w", err)
	}
	return []ReadinessStep{
		{Step: StepTraderDecisions, Condition: "trader の判断（decision_type = 'trader'）", Rows: traders},
		{Step: StepLongShort, Condition: "方向が LONG か SHORT", Rows: longShort},
		{Step: StepLabelV2, Condition: "トリプルバリアのラベル（label_version = 2）がある", Rows: labeled},
		{Step: StepNetReturn, Condition: "net_return_bps がある（= 学習行。再学習が読む行）", Rows: netReturn},
	}, nil
}

// featureGaps lists, per feature, the training rows BuildRowsReport counted as
// lacking it (the rule BuildRows drops a decision by), in FeatureNames order.
func featureGaps(stored, usable, dropped int, missing map[string]int) FeatureGaps {
	names := metalabel.FeatureNames()
	g := FeatureGaps{TrainingRows: stored, UsableRows: usable, DroppedIncomplete: dropped, Missing: make([]FeatureGap, 0, len(names))}
	for _, name := range names {
		g.Missing = append(g.Missing, FeatureGap{Feature: name, MissingRows: missing[name]})
	}
	return g
}
