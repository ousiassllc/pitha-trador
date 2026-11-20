// Package datasetexport builds the versioned, secret-free training dataset of issue #1094:
// one CSV (and Parquet) row per Jev trader decision (decision_type='trader', every direction incl. NONE)
// with its decision-time features, Jev's structured answer, the gate result, every label
// the app stores for it and the shadow / trial answers, written to a local folder.
//
// It is research only: the files are never read by trading code and the dataset is never
// sent anywhere. It reads the database through a read-only query.Q (SELECT only, named
// columns only, never SELECT *) and writes nothing to the database; its only side effect
// is the dataset folder, whose path it derives itself (next to the database file) and
// which no caller can choose. docs/architecture/research-tools/training-dataset.md is the data
// specification, docs/architecture/research-tools/paper-results-mcp-dataset-export.md the design.
package datasetexport

import "os"

// SchemaVersion is the version of the column set, the file layout and the hash spec. Any change
// of a column, its meaning, the files or what the dataset hash covers bumps it.
// "2": Parquet files beside the CSV files, and the period and the split in the dataset hash.
const SchemaVersion = "2"

// Bounds of one export. MaxDecisions bounds the decisions read per call so the whole
// export (the reads, the CSV and the hashing) stays inside the MCP's 3 s budget; more
// decisions in the period is query.ErrLimitExceeded, never a partial dataset.
const (
	MaxDecisions      = 20000
	MaxShadowVersions = 3
	MaxTrialArms      = 4
	MaxEmbargoDays    = 30
)

// Names of the output folder and files.
const (
	ExportsDirName = "exports"
	DatasetIDLen   = 12 // hex characters of the dataset hash in the dataset id
	FileAll        = "dataset.csv"
	FileTrain      = "dataset_train.csv"
	FileEval       = "dataset_eval.csv"
	// The Parquet twin of each CSV file: the same rows and columns (an empty CSV cell is null).
	FileAllParquet   = "dataset.parquet"
	FileTrainParquet = "dataset_train.parquet"
	FileEvalParquet  = "dataset_eval.parquet"
	ManifestName     = "manifest.json"
)

// Permissions of what the export creates.
const (
	DirMode  os.FileMode = 0o700
	FileMode os.FileMode = 0o600
)

// Values of the split column.
const (
	SplitTrain = "train"
	SplitEval  = "eval"
	SplitAll   = "all" // no split was requested
)

// Reasons of a response that wrote nothing (fixed codes, never free text).
const (
	ReasonDisabled        = "export_disabled"
	ReasonAlreadyExported = "already_exported"
	ReasonDirConflict     = "dataset_dir_conflict"
)

// CalibrationHorizons are the horizons (minutes) of the stored label (calibration_outcomes,
// label_version 2) that get columns. The label's timeout is a Settings value of 1..60
// minutes; a stored label at another horizon is not exported and is counted in the manifest.
var CalibrationHorizons = []int{5, 10, 15, 30, 60}

// ResearchHorizons are the fixed horizons of the research label (research_horizon_labels).
var ResearchHorizons = []string{"30m", "60m", "close"}

// Label versions of the three label sources, as stored.
const (
	CalibrationLabelVersion = 2 // triple barrier, net of cost
	ResearchLabelVersion    = 1 // fixed horizon, no barrier
	CounterfactualVersion   = 2 // same definition as the calibration label
)

// marketCloseClock is the TSE close in JST: a "close" research label ends there.
const (
	closeHour   = 15
	closeMinute = 30
)

// Notice is the Japanese note of every response and manifest.
const Notice = "このデータセットはこの PC のローカルフォルダにだけ書き出しました。外部には送っていません。" +
	"データの中身（行）は MCP の応答に含めていません（応答は manifest だけです）。研究用で、取引には使いません"

// NoticeDisabled is the note of the response while the switch is off.
const NoticeDisabled = "学習用データセットの書き出しは無効です（Settings の research.export.enabled が 0。初期値）。何も書き出していません"
