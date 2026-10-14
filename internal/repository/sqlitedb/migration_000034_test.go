package sqlitedb_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/ousiassllc/pitha-trador/db"
)

// Migration 000034 creates metalabel_models and metalabel_predictions (issue
// #757): up/down round-trip, a model's status/AUC and a prediction's
// action/probabilities are CHECK-constrained, a model predicts a decision at
// most once, and down drops both tables.
func TestMigration000034_RoundTripsAndConstrainsMetaLabelTables(t *testing.T) {
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_foreign_keys=1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	src, err := iofs.New(db.MigrationsFS, "migrations")
	if err != nil {
		t.Fatalf("iofs: %v", err)
	}
	drv, err := migratesqlite.WithInstance(conn, &migratesqlite.Config{})
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	exists := func(table string) bool {
		t.Helper()
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil {
			t.Fatalf("sqlite_master: %v", err)
		}
		return n == 1
	}

	if err := m.Migrate(33); err != nil {
		t.Fatalf("migrate to 33: %v", err)
	}
	if exists("metalabel_models") || exists("metalabel_predictions") {
		t.Fatal("meta-label tables exist before migration 34")
	}
	if err := m.Migrate(34); err != nil {
		t.Fatalf("migrate to 34: %v", err)
	}
	if !exists("metalabel_models") || !exists("metalabel_predictions") {
		t.Fatal("meta-label tables missing after migration 34")
	}

	insertModel := func(version, status string, auc any) error {
		_, err := conn.Exec(`INSERT INTO metalabel_models (version, algorithm, label_version, status, trained_at, train_start, train_end,
			oos_start, oos_end, train_samples, oos_samples, feature_names_json, params_json, oos_auc, created_at)
			VALUES (?, 'logreg', 2, ?, 't', 't', 't', 't', 't', 1, 1, '[]', '{}', ?, 't')`, version, status, auc)
		return err
	}
	if err := insertModel("v1", "active", 0.6); err != nil {
		t.Fatalf("insert model: %v", err)
	}
	if err := insertModel("v1", "active", nil); err == nil {
		t.Error("duplicate version accepted, want UNIQUE failure")
	}
	if err := insertModel("v2", "bogus", nil); err == nil {
		t.Error("status 'bogus' accepted, want CHECK failure")
	}
	if err := insertModel("v3", "active", 1.5); err == nil {
		t.Error("oos_auc 1.5 accepted, want CHECK failure")
	}
	if err := insertModel("v4", "rejected", nil); err != nil {
		t.Errorf("NULL oos_auc rejected: %v", err)
	}

	if _, err := conn.Exec(`INSERT INTO instruments (symbol, name, market, is_active, created_at, updated_at) VALUES ('7203', 'x', 'TSE', 1, 't', 't')`); err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO jev_decisions (instrument_id, symbol, timestamp, decision_type, state_hash, state_json, question_version, response_json, latency_ms, model_id, created_at)
		VALUES (1, '7203', 't', 'trader', 'h', '{}', 'trader-v3', '{}', 1, 'm', 't')`); err != nil {
		t.Fatalf("insert decision: %v", err)
	}
	insertPrediction := func(modelID, decisionID int, action string, prob float64) error {
		_, err := conn.Exec(`INSERT INTO metalabel_predictions (model_id, jev_decision_id, predicted_at, raw_probability, calibrated_probability,
			expected_value_bps, action, size_fraction, created_at) VALUES (?, ?, 't', ?, ?, 1, ?, 0, 't')`, modelID, decisionID, prob, prob, action)
		return err
	}
	if err := insertPrediction(1, 1, "skip", 0.4); err != nil {
		t.Fatalf("insert prediction: %v", err)
	}
	if err := insertPrediction(1, 1, "skip", 0.4); err == nil {
		t.Error("second prediction of the same model and decision accepted, want UNIQUE failure")
	}
	if err := insertPrediction(2, 1, "hold", 0.4); err == nil {
		t.Error("action 'hold' accepted, want CHECK failure")
	}
	if err := insertPrediction(2, 1, "take", 1.2); err == nil {
		t.Error("probability 1.2 accepted, want CHECK failure")
	}
	if err := insertPrediction(99, 1, "skip", 0.4); err == nil {
		t.Error("prediction of an unknown model accepted, want FK failure")
	}

	if err := m.Migrate(33); err != nil {
		t.Fatalf("migrate down to 33: %v", err)
	}
	if exists("metalabel_models") || exists("metalabel_predictions") {
		t.Error("meta-label tables still present after down migration")
	}
	if err := m.Migrate(34); err != nil {
		t.Fatalf("migrate up again to 34: %v", err)
	}
}
