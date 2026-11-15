package datasetexport

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// File is one file of the dataset in the manifest.
type File struct {
	Name   string `json:"name"`
	Rows   int    `json:"rows"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// DayRange is the first and last JST decision day of a side (both empty without rows).
type DayRange struct {
	First string `json:"first"`
	Last  string `json:"last"`
}

// LabelInfo says which labels the dataset carries.
type LabelInfo struct {
	Calibration    CalibrationInfo `json:"calibration"`
	Research       ResearchInfo    `json:"research_horizon"`
	Counterfactual CounterInfo     `json:"counterfactual"`
	Note           string          `json:"note"`
}

// CalibrationInfo is the stored label of the app (triple barrier, net of cost).
type CalibrationInfo struct {
	LabelVersion   int   `json:"label_version"`
	HorizonMinutes []int `json:"horizon_minutes"`
	// RowsAtOtherHorizons are stored labels of the period at a horizon with no column (not exported).
	RowsAtOtherHorizons int `json:"rows_at_other_horizons"`
}

// ResearchInfo is the research label (fixed horizon, no barrier).
type ResearchInfo struct {
	LabelVersion int      `json:"label_version"`
	Horizons     []string `json:"horizons"`
}

// CounterInfo is the label of a skipped candidate (same definition as the calibration label).
type CounterInfo struct {
	LabelVersion int `json:"label_version"`
}

// Manifest describes one exported dataset. It is the content of manifest.json and the data of
// the MCP response: no row, no setting and no secret.
type Manifest struct {
	SchemaVersion string `json:"schema_version"`
	DatasetID     string `json:"dataset_id"`
	// DatasetSHA256 is the hash of SchemaVersion and the file hashes (see DatasetHash).
	DatasetSHA256 string `json:"dataset_sha256"`
	CreatedAt     string `json:"created_at"`
	// From and To are the JST days of the period: from is included, to is not.
	From string `json:"from"`
	To   string `json:"to"`
	// SplitAt is null without a split.
	SplitAt     *string   `json:"split_at"`
	EmbargoDays int       `json:"embargo_days"`
	LabelInfo   LabelInfo `json:"label_version"`
	// Decisions is the trader decisions of the period; Rows the rows written. Without a split
	// Rows == Decisions; with one, RowsTrain + RowsEval + RowsInEmbargo + RowsPurged == Decisions.
	Decisions     int `json:"decisions"`
	Rows          int `json:"rows"`
	RowsTrain     int `json:"rows_train"`
	RowsEval      int `json:"rows_eval"`
	RowsInEmbargo int `json:"rows_in_embargo"`
	RowsPurged    int `json:"rows_purged"`
	// TrainDays and EvalDays are the decision days the sides cover (null without a split).
	TrainDays *DayRange `json:"train_days"`
	EvalDays  *DayRange `json:"eval_days"`
	Columns   []string  `json:"columns"`
	// ExcludedColumns are never exported.
	ExcludedColumns []string `json:"excluded_columns"`
	// ShadowVersions and TrialArms say which version / arm each slot column holds, in slot order; the
	// Omitted counts are the versions / arms beyond the cap that have no column.
	ShadowVersions        []string `json:"shadow_versions"`
	ShadowVersionsOmitted int      `json:"shadow_versions_omitted"`
	TrialArms             []string `json:"trial_arms"`
	TrialArmsOmitted      int      `json:"trial_arms_omitted"`
	Files                 []File   `json:"files"`
	SaveDir               string   `json:"save_dir"`
	Notice                string   `json:"notice"`
}

// DatasetHash is the overall hash: sha256 over "<schema_version>\n" followed by every file's hex
// sha256 and "\n", in the order of Manifest.Files (dataset_train.csv, dataset_eval.csv, or
// dataset.csv alone). manifest.json is not part of it (it holds created_at and the path).
func DatasetHash(schemaVersion string, files []File) string {
	h := sha256.New()
	_, _ = h.Write([]byte(schemaVersion + "\n"))
	for _, f := range files {
		_, _ = h.Write([]byte(f.SHA256 + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// DatasetID is "ds-" and the first DatasetIDLen hex characters of the dataset hash.
func DatasetID(datasetSHA256 string) string { return "ds-" + datasetSHA256[:DatasetIDLen] }

func dayRange(e encoded) *DayRange { return &DayRange{First: e.first, Last: e.last} }

func fileOf(e encoded) File {
	return File{Name: e.name, Rows: e.rows, Bytes: len(e.data), SHA256: e.sha256()}
}

func formatCreatedAt(now time.Time) string { return now.UTC().Format(time.RFC3339) }
