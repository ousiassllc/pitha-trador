package exporttests

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// rawColumn finds a raw board, quote or level column of market_snapshots read by name.
var rawColumn = regexp.MustCompile(`\bm\.(bid|ask|price|vwap|volume|turnover|microprice|raw_data_json)\b`)

var readOnlyStatement = regexp.MustCompile(`(?i)^\s*(SELECT|WITH|PRAGMA|BEGIN|ROLLBACK)`)

// The tool changes no table: the statements it issues are reads, none selects a state or raw body, a
// response_json is only ever opened by json_extract / json_type / json_valid of a fixed key, and a write
// on the MCP connection fails.
func TestWritesNoTableAndReadsNoBody(t *testing.T) {
	driverName, counter := testkit.NewCountingDriver(t)
	srv, w := newServer(t, driverName)
	seed(t, w, fixture())
	enableExport(t, w)
	before := dump(t, w)
	if len(before) < 20 {
		t.Fatalf("dump covers %d tables, the check is vacuous", len(before))
	}
	start := len(counter.StatementLog())
	srv.call(t, nil).export(t)
	srv.call(t, map[string]any{"split_at": "2026-10-08", "embargo_days": 2}).export(t)
	srv.call(t, map[string]any{"split_at": "2026-10-05"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	stmts := counter.StatementLog()[start:]
	if len(stmts) < 8 {
		t.Fatalf("only %d statements were logged: the check is vacuous", len(stmts))
	}
	bare := regexp.MustCompile(`response_json`)
	wrapped := regexp.MustCompile(`json_(extract|type|valid)\(d\.response_json`)
	for _, stmt := range stmts {
		if !readOnlyStatement.MatchString(stmt) {
			t.Errorf("a non-read statement ran: %s", testkit.FirstLine(stmt))
		}
		for _, body := range []string{"state_json", "raw_data_json", "error_excerpt", "payload_json", "SELECT *", "bid_depth", "ask_depth"} {
			if strings.Contains(stmt, body) {
				t.Errorf("a statement reads %s: %s", body, testkit.FirstLine(stmt))
			}
		}
		if raw := rawColumn.FindString(stmt); raw != "" {
			t.Errorf("a statement reads the raw column %s: %s", raw, testkit.FirstLine(stmt))
		}
		if n := len(bare.FindAllString(stmt, -1)); n != len(wrapped.FindAllString(stmt, -1)) {
			t.Errorf("response_json is read other than through json_extract/json_type/json_valid: %s", testkit.FirstLine(stmt))
		}
		if strings.Contains(stmt, "runtime_settings") && strings.Contains(stmt, "SELECT key") {
			t.Errorf("all runtime settings are read: %s", testkit.FirstLine(stmt))
		}
	}
	db, err := srv.DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runtime_settings (key, value, updated_at) VALUES ('x', '1', 't')`); err == nil {
		t.Error("a write succeeded on the MCP connection")
	}
	after := dump(t, w)
	for table, h := range before {
		if after[table] != h {
			t.Errorf("table %s changed during the export", table)
		}
	}
}

// dump hashes every row of every table, so any write by the tool changes the result.
func dump(t *testing.T, w *sql.DB) map[string]string {
	t.Helper()
	rows, err := w.QueryContext(t.Context(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	out := map[string]string{}
	for _, n := range names {
		r, err := w.QueryContext(t.Context(), `SELECT * FROM "`+n+`"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := r.Columns()
		h := sha256.New()
		for r.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := r.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			_, _ = fmt.Fprintf(h, "%v\n", vals)
		}
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		_ = r.Close()
		out[n] = fmt.Sprintf("%x", h.Sum(nil))
	}
	return out
}

// assertShape pins the keys of the response: the response's four and the manifest, no row or column data.
func assertShape(t *testing.T, r response) {
	t.Helper()
	var env struct {
		Result struct {
			StructuredContent struct {
				Data map[string]json.RawMessage `json:"data"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatal(err)
	}
	keys := func(m map[string]json.RawMessage) []string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	data := env.Result.StructuredContent.Data
	if got := keys(data); !slices.Equal(got, []string{"enabled", "manifest", "notice", "reason", "written"}) {
		t.Errorf("response keys %v", got)
	}
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal(data["manifest"], &manifest); err != nil {
		t.Fatal(err)
	}
	want := []string{"columns", "created_at", "dataset_id", "dataset_sha256", "decisions", "embargo_days", "eval_days", "excluded_columns", "files", "from",
		"label_version", "notice", "rows", "rows_eval", "rows_in_embargo", "rows_purged", "rows_train", "save_dir", "schema_version", "shadow_versions",
		"shadow_versions_omitted", "split_at", "to", "train_days", "trial_arms", "trial_arms_omitted"}
	if got := keys(manifest); !slices.Equal(got, want) {
		t.Errorf("manifest keys %v, want %v", got, want)
	}
}
