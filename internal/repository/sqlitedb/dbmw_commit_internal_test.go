package sqlitedb

import (
	"context"
	"errors"
	"testing"
)

// failingTx is a driver.Tx whose COMMIT returns a fixed error.
type failingTx struct{ err error }

func (t failingTx) Commit() error { return t.err }
func (failingTx) Rollback() error { return nil }

// TestTxCommit_StorageFailureExtendsStreak covers issue #539: a COMMIT that
// fails with a storage error (SQLITE_READONLY here, standing in for
// FULL/IOERR/BUSY) extends DBWriteFailures, whereas an application-level
// COMMIT error (constraint violation) does not.
func TestTxCommit_StorageFailureExtendsStreak(t *testing.T) {
	conn, err := Open(t.TempDir() + "/pitha.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = conn.Close() }()
	ctx := context.Background()

	// Obtain real *sqlite.Error values by provoking them.
	if _, err := conn.ExecContext(ctx, "CREATE TABLE commit_probe (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO commit_probe (id) VALUES (1)"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	_, constraintErr := conn.ExecContext(ctx, "INSERT INTO commit_probe (id) VALUES (1)")
	if constraintErr == nil || isStorageFailure(constraintErr) {
		t.Fatalf("setup: want a non-storage error, got %v", constraintErr)
	}
	pinned, err := conn.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn: %v", err)
	}
	defer func() { _ = pinned.Close() }()
	if _, err := pinned.ExecContext(ctx, "PRAGMA query_only = ON"); err != nil {
		t.Fatalf("query_only: %v", err)
	}
	_, storageErr := pinned.ExecContext(ctx, "INSERT INTO commit_probe (id) VALUES (2)")
	if storageErr == nil || !isStorageFailure(storageErr) {
		t.Fatalf("setup: want a storage error, got %v", storageErr)
	}

	DBWriteFailures.Succeed()
	var li loggingInterceptor
	if err := li.TxCommit(ctx, failingTx{err: constraintErr}); !errors.Is(err, constraintErr) {
		t.Fatalf("TxCommit error = %v, want the underlying error", err)
	}
	if got := DBWriteFailures.ConsecutiveFailures(); got != 0 {
		t.Fatalf("streak after constraint COMMIT failure = %d, want 0", got)
	}
	for want := 1; want <= 2; want++ {
		if err := li.TxCommit(ctx, failingTx{err: storageErr}); !errors.Is(err, storageErr) {
			t.Fatalf("TxCommit error = %v, want the underlying error", err)
		}
		if got := DBWriteFailures.ConsecutiveFailures(); got != want {
			t.Fatalf("streak after %d storage COMMIT failures = %d", want, got)
		}
	}
	if err := li.TxCommit(ctx, failingTx{}); err != nil {
		t.Fatalf("TxCommit: %v", err)
	}
	if got := DBWriteFailures.ConsecutiveFailures(); got != 0 {
		t.Fatalf("streak after successful COMMIT = %d, want 0", got)
	}
}
