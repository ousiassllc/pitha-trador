package datasetexport

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Params is one export: the period [From, To) (JST midnights), the optional split and the clock.
type Params struct {
	From, To time.Time
	// SplitAt is a JST date YYYY-MM-DD inside the period, or "" for no split.
	SplitAt     string
	EmbargoDays int
	Now         time.Time
}

// Response is what the MCP tool returns: whether the export is on, whether files were written, the
// manifest and why nothing was written. It never holds a row.
type Response struct {
	Enabled bool `json:"enabled"`
	Written bool `json:"written"`
	// Reason is a fixed code (ReasonDisabled, ReasonAlreadyExported, ReasonDirConflict), empty after a write.
	Reason   string    `json:"reason"`
	Notice   string    `json:"notice"`
	Manifest *Manifest `json:"manifest"`
}

// Run builds the dataset of the trader decisions of the period and writes it to
// <database dir>/exports/<dataset_id>/. While the switch is off it writes nothing, not even a
// folder. It returns the response and the number of decisions read. It reads only through q;
// MaxDecisions or more is query.ErrLimitExceeded.
func Run(ctx context.Context, q query.Q, p Params) (Response, int, error) {
	on, err := Enabled(ctx, q)
	if err != nil {
		return Response{}, 0, err
	}
	if !on {
		return Response{Enabled: false, Written: false, Reason: ReasonDisabled, Notice: NoticeDisabled}, 0, nil
	}
	if err := CheckSplit(p.From, p.To, p.SplitAt, p.EmbargoDays); err != nil {
		return Response{}, 0, fmt.Errorf("datasetexport: %w", err)
	}
	base, err := exportsDir(ctx, q)
	if err != nil {
		return Response{}, 0, err
	}
	data, err := collect(ctx, q, p)
	if err != nil {
		return Response{}, 0, err
	}

	m := data.manifest
	m.CreatedAt = formatCreatedAt(p.Now)
	m.Files = make([]File, len(data.files))
	for i, f := range data.files {
		m.Files[i] = fileOf(f)
	}
	m.DatasetSHA256 = DatasetHash(SchemaVersion, m.Files)
	m.DatasetID = DatasetID(m.DatasetSHA256)
	m.SaveDir = filepath.Join(base, m.DatasetID)

	result, final, err := publish(base, m.DatasetID, data.files, m)
	if err != nil {
		return Response{}, 0, err
	}
	resp := Response{Enabled: true, Written: result == published, Notice: Notice, Manifest: &final}
	switch result {
	case identical:
		resp.Reason = ReasonAlreadyExported
	case conflict:
		resp.Reason = ReasonDirConflict
		resp.Manifest = &m
	}
	return resp, m.Decisions, nil
}

// collected is the dataset before it is hashed and written.
type collected struct {
	manifest Manifest
	files    []encoded
}

// collect reads every source of the period and returns the CSV files and the manifest without
// the hashes, the id, the clock and the path.
func collect(ctx context.Context, q query.Q, p Params) (collected, error) {
	from, to := query.Range{From: p.From, To: p.To}.Bounds()
	rows, err := readDecisions(ctx, q, from, to)
	if err != nil {
		return collected{}, err
	}
	byID := make(map[int64]*row, len(rows))
	for _, r := range rows {
		byID[r.id] = r
	}
	counts, err := attachLabels(ctx, q, from, to, byID)
	if err != nil {
		return collected{}, err
	}
	if err := attachGates(ctx, q, from, to, rows, byID); err != nil {
		return collected{}, err
	}
	shadow, trial, shadowOmitted, trialOmitted, err := attachAnswers(ctx, q, from, to, byID)
	if err != nil {
		return collected{}, err
	}

	m := Manifest{
		SchemaVersion: SchemaVersion,
		From:          p.From.In(query.JST).Format(time.DateOnly), To: p.To.In(query.JST).Format(time.DateOnly),
		EmbargoDays: p.EmbargoDays, Decisions: len(rows), Columns: ColumnNames(), ExcludedColumns: ExcludedColumns,
		LabelInfo: LabelInfo{
			Calibration:    CalibrationInfo{LabelVersion: CalibrationLabelVersion, HorizonMinutes: CalibrationHorizons, RowsAtOtherHorizons: counts.CalibrationOtherHorizons},
			Research:       ResearchInfo{LabelVersion: ResearchLabelVersion, Horizons: ResearchHorizons},
			Counterfactual: CounterInfo{LabelVersion: CounterfactualVersion},
			Note:           labelNote,
		},
		ShadowVersions: nonNil(shadow), ShadowVersionsOmitted: shadowOmitted, TrialArms: nonNil(trial), TrialArmsOmitted: trialOmitted,
		Notice: Notice,
	}
	if p.SplitAt == "" {
		for _, r := range rows {
			r.cells[cSplit] = SplitAll
		}
		all, err := encodeCSV(FileAll, rows)
		if err != nil {
			return collected{}, err
		}
		m.Rows = all.rows
		return collected{manifest: m, files: []encoded{all}}, nil
	}

	train, eval, embargo, purged := splitRows(rows, p.SplitAt, p.EmbargoDays)
	trainFile, err := encodeCSV(FileTrain, train)
	if err != nil {
		return collected{}, err
	}
	evalFile, err := encodeCSV(FileEval, eval)
	if err != nil {
		return collected{}, err
	}
	split := p.SplitAt
	m.SplitAt = &split
	m.Rows, m.RowsTrain, m.RowsEval, m.RowsInEmbargo, m.RowsPurged = trainFile.rows+evalFile.rows, trainFile.rows, evalFile.rows, embargo, purged
	m.TrainDays, m.EvalDays = dayRange(trainFile), dayRange(evalFile)
	return collected{manifest: m, files: []encoded{trainFile, evalFile}}, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// labelNote explains how the labels relate to the rows (Japanese, as the notice).
const labelNote = "ラベルは判断の時点より後の値動きから付く（将来の情報）。学習の入力にしてはいけない。" +
	"calib_* は保存済みのトリプルバリア（label_version 2）、rh_* は研究用の固定ホライズン（label_version 1）、" +
	"cf_* は見送り候補の反実仮想（label_version 2）。ラベルが無い判断は該当の列が空"
