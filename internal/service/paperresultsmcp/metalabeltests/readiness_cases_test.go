package metalabeltests

import (
	"database/sql"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/metalabeldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

func funnelRows(d readiness) []int64 {
	var out []int64
	for _, s := range d.Funnel {
		out = append(out, s.Rows)
	}
	return out
}

// Each step of the funnel applies one more of the training conditions of
// repo.ListTrainingRows, and a decision with two label rows counts once.
func TestReadinessFunnelFollowsTheTrainingConditions(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	at := now.Add(-48 * time.Hour)
	mk := func(id int, mod func(*decision)) {
		d := decision{id: id, at: at.Add(time.Duration(id) * time.Minute), direction: "LONG", model: "m",
			state: validState, response: validResponse, horizon: 15, net: 10.0, label: 2}
		ofi := 0.5
		d.ofi, d.micro = &ofi, &ofi
		mod(&d)
		insertDecision(t, w, d)
	}
	mk(1, func(*decision) {})                                       // a training row
	mk(2, func(d *decision) { d.direction = "NONE" })               // not LONG/SHORT
	mk(3, func(d *decision) { d.label = 1 })                        // only the close-only label
	mk(4, func(d *decision) { d.net = nil })                        // no net return
	mk(5, func(d *decision) { d.label = 0 })                        // not labeled yet
	mk(6, func(d *decision) { d.at = now.AddDate(0, 0, -120) })     // before the lookback
	testkit.Decision(t, w, 7, "scout", 0.7, "2026-10-17T00:00:00Z") // not a trader decision
	// A second label row of the decision 1 (another horizon) must not count it twice.
	testkit.MustExec(t, w, `INSERT INTO calibration_outcomes (jev_decision_id, horizon_minutes, future_return,
		max_adverse_excursion, max_favorable_excursion, was_direction_correct, barrier_hit, net_return_bps,
		entry_ref_price, label_version) VALUES (1, 30, 0, 0, 0, 1, 'tp', 5, 100, 2)`)

	d := callReadiness(t, newTestServer(t, path, now), fullLookback)
	// traders 1-5; LONG/SHORT 1,3,4,5; label v2 1,4; net return 1; features 1.
	if got, want := funnelRows(d), []int64{5, 4, 2, 1, 1}; !slices.Equal(got, want) {
		t.Errorf("funnel rows = %v, want %v", got, want)
	}
	if d.Features.TrainingRows != 1 || d.Features.UsableRows != 1 || d.Features.DroppedIncomplete != 0 {
		t.Errorf("features = %+v, want the single training row, complete", d.Features)
	}
}

// The period narrows the decisions read: the default 30 days is not the
// nightly retraining's window (and says so), a period ending in the past is
// read as of its end.
func TestReadinessPeriod(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	for _, d := range learnable(1, 1, now.AddDate(0, 0, -40), "m") { // 40 days ago
		insertDecision(t, w, d)
	}
	for _, d := range learnable(1, 2, now.AddDate(0, 0, -5), "m") { // 5 days ago
		insertDecision(t, w, d)
	}
	srv := newTestServer(t, path, now)

	def := callReadiness(t, srv, map[string]any{})
	if def.MatchesNightlyRetrain || def.Features.TrainingRows != 1 {
		t.Errorf("default period: matches=%v rows=%d, want false and the one row of the last 30 days", def.MatchesNightlyRetrain, def.Features.TrainingRows)
	}
	if def.RowsFrom != "2026-09-18T15:00:00Z" || def.AsOf != now.Format(time.RFC3339) {
		t.Errorf("default window = %s .. %s", def.RowsFrom, def.AsOf)
	}
	full := callReadiness(t, srv, fullLookback)
	if !full.MatchesNightlyRetrain || full.Features.TrainingRows != 2 {
		t.Errorf("full lookback: matches=%v rows=%d, want true and both rows", full.MatchesNightlyRetrain, full.Features.TrainingRows)
	}
	past := callReadiness(t, srv, map[string]any{"from": "2026-07-01", "to": "2026-10-10"})
	if past.MatchesNightlyRetrain || past.AsOf != "2026-10-09T15:00:00Z" || past.Features.TrainingRows != 1 {
		t.Errorf("period ending 2026-10-10: %+v, want as of its JST midnight with the 40-day-old row only", past)
	}
	// More than 366 days, one bound alone and a bad date are the shared period errors.
	for _, a := range []map[string]any{{"from": "2025-01-01", "to": "2026-10-19"}, {"from": "2026-10-01"}, {"from": "x", "to": "y"}} {
		if r := srv.call(t, "metalabel_training_readiness", a); r.status != http.StatusUnprocessableEntity {
			t.Errorf("%v: status %d, want 422", a, r.status)
		}
	}
}

func insertModel(t *testing.T, w *sql.DB, trained time.Time) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO metalabel_models (version, algorithm, label_version, status, reject_reason,
		trained_at, train_start, train_end, oos_start, oos_end, train_samples, oos_samples,
		feature_names_json, params_json, oos_auc, oos_expected_value_bps, oos_calibration_json, created_at)
		VALUES ('v1', 'logreg', 2, 'rejected', 'oos_samples_below_minimum', ?, ?, ?, ?, ?, 1, 1, '[]', '{}', 0.6, 3.5, '[]', ?)`,
		testkit.StoredAt(t, trained), testkit.StoredAt(t, trained), testkit.StoredAt(t, trained), testkit.StoredAt(t, trained),
		testkit.StoredAt(t, trained), testkit.StoredAt(t, trained))
}

func insertSkip(t *testing.T, w *sql.DB, at time.Time, usable int) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, created_at)
		VALUES (?, 'too_few_rows_before_oos', ?, 130, ?)`, testkit.StoredAt(t, at), usable, testkit.StoredAt(t, at))
}

// hasDroppedColumn reports whether this schema's metalabel_retrain_skips has
// the dropped_incomplete column (added by the migration of issue #862, nullable:
// NULL is "no value recorded"). The tests below follow the schema they are run
// against and never add or drop the column themselves.
func hasDroppedColumn(t *testing.T, w *sql.DB) bool {
	t.Helper()
	var n int
	if err := w.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('metalabel_retrain_skips') WHERE name = 'dropped_incomplete'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func insertSkipDropped(t *testing.T, w *sql.DB, at time.Time, usable, dropped int) {
	t.Helper()
	testkit.MustExec(t, w, `INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, dropped_incomplete, created_at)
		VALUES (?, 'too_few_rows_before_oos', ?, 130, ?, ?)`, testkit.StoredAt(t, at), usable, dropped, testkit.StoredAt(t, at))
}

// What the last retraining left behind, and what is said when it left no
// dropped_incomplete, in the schema the test runs against (hasDroppedColumn):
// without the column the reason is not_recorded_in_schema; with it, a skip
// recorded before the value existed (NULL) is not_recorded_on_row and is never
// read as 0, while a recorded value, 0 included, is returned as it is.
func TestReadinessLastRetrain(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	srv := newTestServer(t, path, now)

	d := callReadiness(t, srv, fullLookback)
	if d.LastRetrain.LastRun != "none" || d.LastRetrain.LatestSkip != nil || d.LastRetrain.DroppedIncompleteState != metalabeldiag.DroppedNoSkip {
		t.Errorf("empty database: %+v", d.LastRetrain)
	}

	insertSkip(t, w, now.Add(-10*time.Hour), 80) // names no dropped_incomplete: NULL where the column exists
	d = callReadiness(t, srv, fullLookback)
	s := d.LastRetrain.LatestSkip
	if d.LastRetrain.LastRun != "skipped" || s == nil || s.UsableRows != 80 || s.RequiredRows != 130 || s.ReasonCode != "too_few_rows_before_oos" || s.DroppedIncomplete != nil {
		t.Fatalf("skip with no dropped_incomplete: %+v", d.LastRetrain)
	}
	if hasDroppedColumn(t, w) {
		if d.LastRetrain.DroppedIncompleteState != metalabeldiag.DroppedNotOnRow {
			t.Errorf("schema with the column, NULL value: state = %q, want %q", d.LastRetrain.DroppedIncompleteState, metalabeldiag.DroppedNotOnRow)
		}
		insertSkipDropped(t, w, now.Add(-9*time.Hour), 80, 77)
		d = callReadiness(t, srv, fullLookback)
		if s = d.LastRetrain.LatestSkip; s == nil || s.DroppedIncomplete == nil || *s.DroppedIncomplete != 77 || d.LastRetrain.DroppedIncompleteState != metalabeldiag.DroppedRecorded {
			t.Errorf("recorded value 77: %+v", d.LastRetrain)
		}
		insertSkipDropped(t, w, now.Add(-8*time.Hour), 80, 0)
		d = callReadiness(t, srv, fullLookback)
		if s = d.LastRetrain.LatestSkip; s == nil || s.DroppedIncomplete == nil || *s.DroppedIncomplete != 0 || d.LastRetrain.DroppedIncompleteState != metalabeldiag.DroppedRecorded {
			t.Errorf("recorded value 0 must stay 0, not become not-recorded: %+v", d.LastRetrain)
		}
	} else if d.LastRetrain.DroppedIncompleteState != metalabeldiag.DroppedNotInSchema {
		t.Errorf("schema without the column: state = %q, want %q", d.LastRetrain.DroppedIncompleteState, metalabeldiag.DroppedNotInSchema)
	}

	// A model stored after the skip means the last night learned.
	insertModel(t, w, now.Add(-time.Hour))
	d = callReadiness(t, srv, fullLookback)
	if d.LastRetrain.LastRun != "model_stored" || d.LastRetrain.LatestModelTrainedAt == nil || d.LastRetrain.DroppedIncompleteState != metalabeldiag.DroppedNoSkip {
		t.Errorf("model after skip: %+v", d.LastRetrain)
	}
}

// Every state of dropped_incomplete, whichever schema the migrations give: a
// separate in-memory database with metalabel_retrain_skips built here with and
// without the column (nullable, as in issue #862), so no test alters the
// migrated schema.
func TestReadLastRetrainEveryDroppedIncompleteState(t *testing.T) {
	at := "2026-10-17T18:00:00.000000000Z"
	for _, c := range []struct {
		name      string
		column    string // the column definition, empty for none
		insertSQL string
		want      string
		wantValue *int64
	}{
		{"no column", "", `INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows) VALUES (?, 'too_few_rows_before_oos', 80, 130)`, metalabeldiag.DroppedNotInSchema, nil},
		{"column, NULL", ", dropped_incomplete INTEGER", `INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows) VALUES (?, 'too_few_rows_before_oos', 80, 130)`, metalabeldiag.DroppedNotOnRow, nil},
		{"column, 77", ", dropped_incomplete INTEGER", `INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, dropped_incomplete) VALUES (?, 'too_few_rows_before_oos', 80, 130, 77)`, metalabeldiag.DroppedRecorded, ptr64(77)},
		{"column, 0", ", dropped_incomplete INTEGER", `INSERT INTO metalabel_retrain_skips (skipped_at, reason_code, usable_rows, required_rows, dropped_incomplete) VALUES (?, 'too_few_rows_before_oos', 80, 130, 0)`, metalabeldiag.DroppedRecorded, ptr64(0)},
	} {
		t.Run(c.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = db.Close() })
			testkit.MustExec(t, db, `CREATE TABLE metalabel_retrain_skips (id INTEGER PRIMARY KEY AUTOINCREMENT, skipped_at TEXT NOT NULL,
				reason_code TEXT NOT NULL, usable_rows INTEGER NOT NULL, required_rows INTEGER NOT NULL`+c.column+`)`)
			testkit.MustExec(t, db, `CREATE TABLE metalabel_models (id INTEGER PRIMARY KEY AUTOINCREMENT, trained_at TEXT NOT NULL)`)
			testkit.MustExec(t, db, c.insertSQL, at)

			got, err := metalabeldiag.ReadLastRetrain(t.Context(), db)
			if err != nil {
				t.Fatal(err)
			}
			if got.DroppedIncompleteState != c.want || got.LatestSkip == nil {
				t.Fatalf("state = %q, skip = %+v; want %q", got.DroppedIncompleteState, got.LatestSkip, c.want)
			}
			if v := got.LatestSkip.DroppedIncomplete; (v == nil) != (c.wantValue == nil) || (v != nil && *v != *c.wantValue) {
				t.Errorf("dropped_incomplete = %v, want %v", v, c.wantValue)
			}
		})
	}
}

func ptr64(v int64) *int64 { return &v }

// The tool reports the cutoff the retraining would use: the stored value and
// its status, and the OOS window never opens before it.
func TestReadinessUsesTheStoredCutoff(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	history(t, w, now, 30, 30)
	srv := newTestServer(t, path, now)

	d := callReadiness(t, srv, fullLookback)
	if d.Split.LLMCutoff != "2026-09-01" || d.Split.LLMCutoffStatus != "unverified" {
		t.Errorf("cutoff = %s (%s)", d.Split.LLMCutoff, d.Split.LLMCutoffStatus)
	}
	// The OOS window opens at the cutoff when that is later than the default
	// start: only the 8 rows on or after 2026-10-18 00:00 UTC are OOS.
	testkit.Setting(t, w, "metalabel.llm_cutoff", `"2026-10-18"`)
	d = callReadiness(t, srv, fullLookback)
	if d.Split.OOS.Rows != 8 || d.Split.OOSStart != "2026-10-18T00:00:00Z" {
		t.Errorf("cutoff 2026-10-18: oos = %+v from %s, want the 8 rows from the cutoff", d.Split.OOS, d.Split.OOSStart)
	}
	// A cutoff after every row leaves no OOS rows at all.
	testkit.Setting(t, w, "metalabel.llm_cutoff", `"2026-10-19"`)
	d = callReadiness(t, srv, fullLookback)
	if d.Split.OOS.Rows != 0 || d.Split.LLMCutoff != "2026-10-19" {
		t.Errorf("cutoff 2026-10-19: oos = %+v", d.Split.OOS)
	}
}
