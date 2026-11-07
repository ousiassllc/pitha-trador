package metalabel_test

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

func setDecisionModel(t *testing.T, db *sql.DB, id int64, model string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `UPDATE jev_decisions SET model_id = ? WHERE id = ?`, model, id); err != nil {
		t.Fatalf("set model_id: %v", err)
	}
}

// A window in which Jev's model changed (jev-1.13.0 then jev-1.14.0, the
// 2026-11-04 retraining) is read as one model: the one that answered last.
// ListTrainingRows and CountLabeledDecisions apply the same scope, and the
// latest scout answer, a decision after the window end and a foreign-prompt
// decision do not pick the model.
func TestRepository_TrainingRowsAndCountsLimitToTheLastAnsweringJevModel(t *testing.T) {
	db := newTestDB(t)
	r := metalabel.NewRepository(db)
	ctx := context.Background()
	res, err := db.ExecContext(ctx, `INSERT INTO instruments (symbol, name, market, is_active) VALUES ('7203', 'x', 'TSE Prime', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := res.LastInsertId()
	base := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	to := base.Add(10 * time.Hour)

	var old, current []int64
	add := func(at time.Duration, direction, decisionType, model string) int64 {
		id := insertTrainingDecision(t, db, inst, "7203", base.Add(at), direction, decisionType)
		setDecisionModel(t, db, id, model)
		insertOutcome(t, db, id, 15, 2, 3.0)
		return id
	}
	old = append(old, add(0, "LONG", "trader", "jev-1.13.0"), add(time.Hour, "SHORT", "trader", "jev-1.13.0"))
	current = append(current, add(2*time.Hour, "LONG", "trader", "jev-1.14.0"), add(3*time.Hour, "SHORT", "trader", "jev-1.14.0"))
	// Decisions that must not choose the model: the last NONE trader decision
	// is a real answer (below), but a scout answer, a decision at or after the
	// window end and a foreign-prompt decision are not.
	add(4*time.Hour, "LONG", "scout", "jev-9-scout")
	add(10*time.Hour, "LONG", "trader", "jev-9-after")
	foreign := insertTrainingDecision(t, db, inst, "7203", base.Add(5*time.Hour), "LONG", "trader")
	setDecisionModel(t, db, foreign, "jev-9-foreign")
	if _, err := db.ExecContext(ctx, `UPDATE jev_decisions SET question_version = 'trader-v9' WHERE id = ?`, foreign); err != nil {
		t.Fatal(err)
	}
	r.SetForeignTraderVersions(func(context.Context) ([]string, error) { return []string{"trader-v9"}, nil })

	ids := func(rows []metalabel.TrainingRow) []int64 {
		var out []int64
		for _, row := range rows {
			if row.JevModelID != "jev-1.14.0" {
				t.Errorf("row %d is of model %q, want only jev-1.14.0", row.JevDecisionID, row.JevModelID)
			}
			out = append(out, row.JevDecisionID)
		}
		return out
	}
	rows, err := r.ListTrainingRows(ctx, base, to)
	if err != nil {
		t.Fatalf("ListTrainingRows: %v", err)
	}
	if got := ids(rows); !slices.Equal(got, current) {
		t.Errorf("ListTrainingRows ids = %v, want %v (jev-1.14.0 only; %v are jev-1.13.0)", got, current, old)
	}
	counts, err := r.CountLabeledDecisions(ctx, base, to)
	if err != nil {
		t.Fatalf("CountLabeledDecisions: %v", err)
	}
	if counts.V2 != len(current) {
		t.Errorf("CountLabeledDecisions V2 = %d, want %d (the rows ListTrainingRows returns)", counts.V2, len(current))
	}

	// Seen from the earlier instant, the last answer was jev-1.13.0's.
	rows, err = r.ListTrainingRows(ctx, base, base.Add(2*time.Hour))
	if err != nil || len(rows) != len(old) || rows[0].JevModelID != "jev-1.13.0" {
		t.Errorf("ListTrainingRows before the model change = %+v, %v, want the %d jev-1.13.0 rows", rows, err, len(old))
	}
}

func TestResolveTrainingScope(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	res, err := db.ExecContext(ctx, `INSERT INTO instruments (symbol, name, market, is_active) VALUES ('7203', 'x', 'TSE Prime', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := res.LastInsertId()
	base := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)

	// No decision: no model filter, and no SQL condition at all.
	scope, err := metalabel.ResolveTrainingScope(ctx, db, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cond, args := scope.Where(); scope.JevModelID != "" || cond != "" || args != nil {
		t.Errorf("scope of an empty database = %+v, where %q %v, want no filter", scope, cond, args)
	}

	// The latest trader decision picks the model even when it is NONE (it is a
	// real answer of the current model) and has no label yet.
	a := insertTrainingDecision(t, db, inst, "7203", base, "LONG", "trader")
	setDecisionModel(t, db, a, "jev-1.13.0")
	b := insertTrainingDecision(t, db, inst, "7203", base.Add(time.Hour), "NONE", "trader")
	setDecisionModel(t, db, b, "jev-1.14.0")
	scope, err = metalabel.ResolveTrainingScope(ctx, db, base.Add(2*time.Hour), []string{"trader-v9"})
	if err != nil {
		t.Fatal(err)
	}
	cond, args := scope.Where()
	if scope.JevModelID != "jev-1.14.0" || cond != "d.question_version NOT IN (?) AND d.model_id = ?" || !slices.Equal(args, []any{"trader-v9", "jev-1.14.0"}) {
		t.Errorf("scope = %+v, where %q %v, want the version leave-out and model_id = jev-1.14.0", scope, cond, args)
	}
}
