package datasetexport

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The whitelist is well formed: a name once, every column documented, none of them an excluded
// name, and the groups sit where the row builder writes them.
func TestColumnWhitelistIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Columns {
		if seen[c.Name] {
			t.Errorf("column %s is listed twice", c.Name)
		}
		seen[c.Name] = true
		if c.Source == "" || c.Meaning == "" {
			t.Errorf("column %s has no source or meaning: %+v", c.Name, c)
		}
		if slices.Contains(ExcludedColumns, c.Name) {
			t.Errorf("column %s is also an excluded column", c.Name)
		}
	}
	if len(Columns) != off.total || !slices.Equal(ColumnNames(), namesOf(Columns)) {
		t.Errorf("%d columns, the row has %d cells", len(Columns), off.total)
	}
	at := func(i int, name string) {
		if Columns[i].Name != name {
			t.Errorf("column %d is %s, want %s: a group moved", i, Columns[i].Name, name)
		}
	}
	at(cDecisionID, "decision_id")
	at(cSplit, "split")
	at(off.decision, "answer_direction")
	at(off.bar, "spread_bps")
	at(off.hasBar, "has_bar")
	at(off.instrument, "sector")
	at(off.gate+gOrigin, "origin")
	at(off.calibration, "calib_h5_net_bps")
	at(off.research, "rh_30m_long_net_bps")
	at(off.counterfactual+cfCutoffUnverified, "cf_cutoff_unverified")
	at(off.shadow, "shadow1_version")
	at(off.trial, "trial1_arm_id")
	at(off.total-1, "trial4_confidence")
}

func namesOf(cols []Column) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Name
	}
	return out
}

// The SQL of the decisions query is generated from fixed fields only and reads none of the raw board,
// quote or level columns, nor a body: a source column added by mistake to the field lists fails here.
func TestFieldSQLReadsNoRawColumn(t *testing.T) {
	banned := []string{"bid", "ask", "bid_depth", "ask_depth", "microprice", "raw_data_json", "price", "vwap", "volume", "turnover", "state_json", "error_excerpt", "reject_reason"}
	for _, f := range slices.Concat(decisionFields, barFields, instrumentFields) {
		col := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(f.expr, "d."), "m."), "i.")
		if slices.Contains(banned, col) {
			t.Errorf("field %s reads the raw column %s", f.name, col)
		}
		if f.json != "" && f.expr != "" {
			t.Errorf("field %s has both a column and a json key", f.name)
		}
		sql := f.selectSQL()
		if !strings.HasPrefix(sql, "CASE WHEN ") {
			t.Errorf("field %s is not guarded: %s", f.name, sql)
		}
		if f.json != "" && (!strings.Contains(sql, "json_valid(d.response_json)") || strings.Contains(sql, "state_json")) {
			t.Errorf("field %s reads the answer unguarded: %s", f.name, sql)
		}
	}
	// The JSON keys read are exactly Jev's structured answer.
	var keys []string
	for _, f := range decisionFields {
		if f.json != "" {
			keys = append(keys, f.json)
		}
	}
	want := []string{"regime", "entry_quality", "toxic_flow", "liquidity_stressed", "continuation_probability"}
	if !slices.Equal(keys, want) {
		t.Errorf("response_json keys read: %v, want %v", keys, want)
	}
}

// docs/architecture/research-tools/training-dataset.md names every column and every excluded column: a column
// added without its documentation fails.
func TestDataSpecificationListsEveryColumn(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "architecture", "research-tools", "training-dataset.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	for _, c := range Columns {
		if !strings.Contains(doc, "`"+c.Name+"`") {
			t.Errorf("docs/architecture/research-tools/training-dataset.md does not list the column %s", c.Name)
		}
	}
	for _, name := range ExcludedColumns {
		if !strings.Contains(doc, "`"+name+"`") {
			t.Errorf("docs/architecture/research-tools/training-dataset.md does not list the excluded column %s", name)
		}
	}
	for _, want := range []string{"schema_version", "dataset_sha256", "rows_in_embargo", "rows_purged", "label_version"} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/architecture/research-tools/training-dataset.md does not describe %s", want)
		}
	}
}
