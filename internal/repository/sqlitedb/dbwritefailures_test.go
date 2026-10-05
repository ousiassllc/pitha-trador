package sqlitedb_test

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// TestDBWriteFailures_CountsStorageFailuresOnly covers FR-RISK-2's
// "DB書き込み失敗が一定回数継続" signal: a write the database cannot
// perform (here: a read-only connection) extends the streak, a
// successful write resets it, and an application-level error (a
// constraint violation) does neither.
func TestDBWriteFailures_CountsStorageFailuresOnly(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn: %v", err)
	}
	defer func() { _ = conn.Close() }()

	streak := sqlitedb.DBWriteFailures
	streak.Succeed()

	if _, err := conn.ExecContext(ctx, "CREATE TABLE streak_probe (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create probe table: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO streak_probe (id) VALUES (1)"); err != nil {
		t.Fatalf("insert probe row: %v", err)
	}
	if got := streak.ConsecutiveFailures(); got != 0 {
		t.Fatalf("streak after successful writes = %d, want 0", got)
	}

	// Constraint violation: not a storage failure.
	if _, err := conn.ExecContext(ctx, "INSERT INTO streak_probe (id) VALUES (1)"); err == nil {
		t.Fatal("duplicate insert unexpectedly succeeded")
	}
	if got := streak.ConsecutiveFailures(); got != 0 {
		t.Fatalf("streak after constraint violation = %d, want 0", got)
	}

	if _, err := conn.ExecContext(ctx, "PRAGMA query_only = ON"); err != nil {
		t.Fatalf("enable query_only: %v", err)
	}
	for want := 1; want <= 3; want++ {
		if _, err := conn.ExecContext(ctx, "INSERT INTO streak_probe (id) VALUES (2)"); err == nil {
			t.Fatal("write on a read-only connection unexpectedly succeeded")
		}
		if got := streak.ConsecutiveFailures(); got != want {
			t.Fatalf("streak after %d storage failures = %d", want, got)
		}
	}

	if _, err := conn.ExecContext(ctx, "PRAGMA query_only = OFF"); err != nil {
		t.Fatalf("disable query_only: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO streak_probe (id) VALUES (2)"); err != nil {
		t.Fatalf("insert after recovery: %v", err)
	}
	if got := streak.ConsecutiveFailures(); got != 0 {
		t.Fatalf("streak after recovery = %d, want 0", got)
	}
}

// TestDBQueryLogLevel pins the level the sqlmw interceptor logs at: a fast
// successful query must stay below INFO (idle scheduler polling issues
// ~30 of them per second), a slow one is WARN and a failed one is ERROR.
func TestDBQueryLogLevel(t *testing.T) {
	db := newTestDB(t)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx := context.Background()
	_, _ = db.ExecContext(ctx, "SELECT 1 /* fast */")
	_, _ = db.ExecContext(ctx, "WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c WHERE x < 5000000) SELECT count(*) FROM c /* slow */")
	_, _ = db.ExecContext(ctx, "SELECT * FROM no_such_table /* failing */")

	levels := map[string]string{}
	for _, line := range strings.Split(buf.String(), "\n") {
		for _, tag := range []string{"fast", "slow", "failing"} {
			if strings.Contains(line, "/* "+tag+" */") {
				levels[tag] = strings.Fields(strings.SplitN(line, "level=", 2)[1])[0]
			}
		}
	}
	want := map[string]string{"fast": "DEBUG", "slow": "WARN", "failing": "ERROR"}
	for tag, lvl := range want {
		if levels[tag] != lvl {
			t.Errorf("%s query logged at %q, want %q\nlog:\n%s", tag, levels[tag], lvl, buf.String())
		}
	}
}

// TestDBWriteFailures_BeginAndCommit covers issue #539: a BEGIN IMMEDIATE
// that cannot take the write lock extends the streak (it is the first
// statement of every fill/close/enqueue transaction), while a successful
// read-write COMMIT resets it. A read-only transaction's COMMIT writes
// nothing and must not mask failures.
func TestDBWriteFailures_BeginAndCommit(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	streak := sqlitedb.DBWriteFailures

	holder, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("holder Conn: %v", err)
	}
	defer func() { _ = holder.Close() }()
	waiter, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("waiter Conn: %v", err)
	}
	defer func() { _ = waiter.Close() }()

	if _, err := waiter.ExecContext(ctx, "PRAGMA busy_timeout = 20"); err != nil {
		t.Fatalf("shorten busy_timeout: %v", err)
	}
	if _, err := holder.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("holder BEGIN IMMEDIATE: %v", err)
	}

	streak.Succeed()
	for want := 1; want <= 2; want++ {
		if tx, err := waiter.BeginTx(ctx, nil); err == nil {
			_ = tx.Rollback()
			t.Fatal("BEGIN IMMEDIATE unexpectedly succeeded while another connection holds the write lock")
		}
		if got := streak.ConsecutiveFailures(); got != want {
			t.Fatalf("streak after %d busy BEGINs = %d", want, got)
		}
	}

	if _, err := holder.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatalf("holder ROLLBACK: %v", err)
	}
	// The holder's own successful Exec reset the streak; rebuild it.
	streak.Fail()
	streak.Fail()

	// A read-only transaction commits without writing: the streak stays.
	roTx, err := waiter.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("read-only BeginTx: %v", err)
	}
	if err := roTx.Commit(); err != nil {
		t.Fatalf("read-only Commit: %v", err)
	}
	if got := streak.ConsecutiveFailures(); got != 2 {
		t.Fatalf("streak after read-only COMMIT = %d, want 2 (unchanged)", got)
	}

	// A successful read-write COMMIT resets it.
	tx, err := waiter.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx after lock release: %v", err)
	}
	if got := streak.ConsecutiveFailures(); got != 2 {
		t.Fatalf("streak after successful BEGIN = %d, want 2 (unchanged)", got)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if got := streak.ConsecutiveFailures(); got != 0 {
		t.Fatalf("streak after successful COMMIT = %d, want 0", got)
	}
}
