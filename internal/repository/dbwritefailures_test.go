package repository_test

import (
	"context"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository"
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

	streak := repository.DBWriteFailures
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
