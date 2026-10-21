package metalabeldiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// readinessSplit runs training.SplitWalkForward with the default configs on
// rows as of asOf and reports both sides against their required counts. The
// real call decides train_ready and the skip code; when it fails, the OOS side
// is still counted (describeOOS) so a skipped night shows both sides.
func readinessSplit(rows []training.Row, asOf, cutoff time.Time) (ReadinessSplit, error) {
	walk, bar := training.DefaultWalkForwardConfig(), training.DefaultAcceptanceConfig()
	s := ReadinessSplit{
		Train: TrainSide{
			RequiredRows: walk.MinFit + walk.MinCalibration, RequiredFitRows: walk.MinFit, RequiredCalibrationRows: walk.MinCalibration,
		},
		OOS: OOSSide{RequiredRows: bar.MinOOSSamples, RequiredDays: bar.MinOOSDays, RequiredTake: bar.MinTakes},
	}
	split, err := training.SplitWalkForward(rows, asOf, cutoff, walk)
	var oos []training.Row
	if err == nil {
		fit, cal := len(split.Fit), len(split.Calibrate)
		s.TrainReady = true
		s.Train.Rows, s.Train.FitRows, s.Train.CalibrationRows = fit+cal, &fit, &cal
		s.OOSStart, oos = split.OOSStart.UTC().Format(time.RFC3339), split.OOS
	} else {
		var short *training.InsufficientDataError
		if !errors.As(err, &short) {
			return ReadinessSplit{}, fmt.Errorf("query: walk-forward split: %w", err)
		}
		s.TrainSkipCode = &short.Code
		s.Train.Rows = short.Usable
		start, rowsOOS, derr := describeOOS(rows, asOf, cutoff, walk)
		if derr != nil {
			return ReadinessSplit{}, derr
		}
		s.OOSStart, oos = start.UTC().Format(time.RFC3339), rowsOOS
	}
	s.OOS.Rows, s.OOS.Days = len(oos), distinctJSTDays(oos)
	s.OOS.Ready = s.OOS.Rows >= bar.MinOOSSamples && s.OOS.Days >= bar.MinOOSDays
	return s, nil
}

// describeOOS returns the OOS window start and rows the split uses for rows,
// for a night the real split refuses (too few fit/calibration rows). It runs
// the same SplitWalkForward with the fit and calibration minimums lowered to
// 1 and two ancient padding rows so the fit/calibration side can always be
// formed; the padding sits before every window and is never an OOS row, so
// the OOS window and rows are exactly the real split's.
func describeOOS(rows []training.Row, asOf, cutoff time.Time, walk training.WalkForwardConfig) (time.Time, []training.Row, error) {
	walk.MinFit, walk.MinCalibration = 1, 1
	ancient := asOf.AddDate(-100, 0, 0)
	later := ancient.Add(2 * time.Hour) // far enough apart that neither is purged
	padded := append([]training.Row{{DecidedAt: ancient, LabelEnd: ancient}, {DecidedAt: later, LabelEnd: later}}, rows...)
	split, err := training.SplitWalkForward(padded, asOf, cutoff, walk)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("query: walk-forward OOS window: %w", err)
	}
	return split.OOSStart, split.OOS, nil
}

// distinctJSTDays is the number of query.JST dates the rows' decisions fall on, what
// the adoption bar counts as OOS trading days.
func distinctJSTDays(rows []training.Row) int {
	days := map[string]struct{}{}
	for _, r := range rows {
		days[r.DecidedAt.In(query.JST).Format(time.DateOnly)] = struct{}{}
	}
	return len(days)
}

// lastRetrain reads the newest retrain skip and the newest stored model. The
// skip's dropped_incomplete is read only when the column exists (issue #862
// adds it); a database without it reports null and DroppedNotInSchema.
func lastRetrain(ctx context.Context, q query.Q) (LastRetrain, error) {
	var hasDropped int
	if err := q.QueryRowContext(ctx, `
SELECT COUNT(*) FROM pragma_table_info('metalabel_retrain_skips') WHERE name = 'dropped_incomplete'`).Scan(&hasDropped); err != nil {
		return LastRetrain{}, fmt.Errorf("query: retrain skip columns: %w", err)
	}
	out := LastRetrain{LastRun: "none", DroppedIncompleteState: DroppedNoSkip}
	droppedColumn := "NULL"
	if hasDropped > 0 {
		droppedColumn = "dropped_incomplete"
	}
	var (
		skip      LastRetrainSkip
		skippedAt string
		dropped   sql.NullInt64
	)
	err := q.QueryRowContext(ctx, `
SELECT skipped_at, reason_code, usable_rows, required_rows, `+droppedColumn+`
FROM metalabel_retrain_skips ORDER BY skipped_at DESC, id DESC LIMIT 1`).
		Scan(&skippedAt, &skip.ReasonCode, &skip.UsableRows, &skip.RequiredRows, &dropped)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		skippedAt = ""
	case err != nil:
		return LastRetrain{}, fmt.Errorf("query: latest retrain skip: %w", err)
	default:
		skip.SkippedAt, skip.ReasonCode = query.FormatOut(skippedAt), query.NormalizeRetrainSkipCode(skip.ReasonCode)
		switch {
		case hasDropped == 0:
			out.DroppedIncompleteState = DroppedNotInSchema
		case dropped.Valid:
			skip.DroppedIncomplete = &dropped.Int64
			out.DroppedIncompleteState = DroppedRecorded
		default:
			out.DroppedIncompleteState = DroppedNotOnRow
		}
		out.LatestSkip = &skip
	}
	var trainedAt string
	err = q.QueryRowContext(ctx, `
SELECT trained_at FROM metalabel_models ORDER BY trained_at DESC, id DESC LIMIT 1`).Scan(&trainedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		trainedAt = ""
	case err != nil:
		return LastRetrain{}, fmt.Errorf("query: latest metalabel model: %w", err)
	default:
		t := query.FormatOut(trainedAt)
		out.LatestModelTrainedAt = &t
	}
	switch {
	case skippedAt != "" && skippedAt >= trainedAt:
		out.LastRun = "skipped"
	case trainedAt != "":
		out.LastRun, out.DroppedIncompleteState = "model_stored", DroppedNoSkip
	}
	return out, nil
}
