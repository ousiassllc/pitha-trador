package exporttests

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/datasetexport"
)

// Every part of the specification is in the dataset hash: the schema version, from, to, split_at,
// embargo_days and each file's name and hash; and the same input gives the same hash.
func TestDatasetHashCoversTheSpecificationAndTheFiles(t *testing.T) {
	base := datasetexport.Spec{From: "2026-10-05", To: "2026-10-12", SplitAt: "2026-10-08", EmbargoDays: 1}
	files := []datasetexport.File{{Name: "a.csv", SHA256: "aa"}, {Name: "a.parquet", SHA256: "bb"}}
	want := datasetexport.DatasetHash("2", base, files)
	if datasetexport.DatasetHash("2", base, files) != want {
		t.Fatal("the hash is not stable")
	}
	for name, got := range map[string]string{
		"schema":     datasetexport.DatasetHash("3", base, files),
		"from":       datasetexport.DatasetHash("2", datasetexport.Spec{From: "2026-10-04", To: base.To, SplitAt: base.SplitAt, EmbargoDays: 1}, files),
		"to":         datasetexport.DatasetHash("2", datasetexport.Spec{From: base.From, To: "2026-10-13", SplitAt: base.SplitAt, EmbargoDays: 1}, files),
		"split_at":   datasetexport.DatasetHash("2", datasetexport.Spec{From: base.From, To: base.To, SplitAt: "2026-10-09", EmbargoDays: 1}, files),
		"no split":   datasetexport.DatasetHash("2", datasetexport.Spec{From: base.From, To: base.To, EmbargoDays: 1}, files),
		"embargo":    datasetexport.DatasetHash("2", datasetexport.Spec{From: base.From, To: base.To, SplitAt: base.SplitAt, EmbargoDays: 2}, files),
		"file hash":  datasetexport.DatasetHash("2", base, []datasetexport.File{{Name: "a.csv", SHA256: "aa"}, {Name: "a.parquet", SHA256: "cc"}}),
		"file name":  datasetexport.DatasetHash("2", base, []datasetexport.File{{Name: "b.csv", SHA256: "aa"}, {Name: "a.parquet", SHA256: "bb"}}),
		"file order": datasetexport.DatasetHash("2", base, []datasetexport.File{files[1], files[0]}),
		"one file":   datasetexport.DatasetHash("2", base, files[:1]),
	} {
		if got == want {
			t.Errorf("a change of %s does not change the dataset hash", name)
		}
	}
}

// The manifest's own spec is what the export was asked for, a missing split being "".
func TestManifestSpecIsThePeriodAndSplitAsked(t *testing.T) {
	split := "2026-10-08"
	got := datasetexport.Manifest{From: "2026-10-05", To: "2026-10-12", SplitAt: &split, EmbargoDays: 2}.Spec()
	if got != (datasetexport.Spec{From: "2026-10-05", To: "2026-10-12", SplitAt: split, EmbargoDays: 2}) {
		t.Errorf("spec %+v", got)
	}
	if got := (datasetexport.Manifest{From: "a", To: "b"}).Spec(); got.SplitAt != "" || got.EmbargoDays != 0 {
		t.Errorf("spec without a split %+v", got)
	}
}
