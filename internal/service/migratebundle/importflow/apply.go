package importflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/backup"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
)

// Result statuses of last-result.json.
const (
	StatusApplied = "applied"
	StatusFailed  = "failed"
)

// Result is what a start-up apply did, kept in <db dir>/migration/last-result.json
// for the Settings screen.
type Result struct {
	Status string `json:"status"`
	// At is when the apply ran (UTC RFC 3339).
	At string `json:"at"`
	// Error is why a failed apply did not replace the database (the current database is unchanged).
	Error string `json:"error,omitempty"`
	// SafetyCopy is the copy of the replaced database ("" when there was none to copy).
	SafetyCopy      string `json:"safety_copy,omitempty"`
	BundleCreatedAt string `json:"bundle_created_at,omitempty"`
	BundleVersion   string `json:"bundle_app_version,omitempty"`
}

// ApplyPending applies a confirmed import (Service.Confirm) to the database
// file at dbPath. It MUST run before the database is opened for the
// application (bootstrap.Run does, after the single-instance lock). It returns
// nil when nothing is pending. Otherwise it returns the Result it also
// records: on failure the current database is exactly as it was (the safety
// copy, when already taken, is kept), the pending import is discarded so the
// next start does not retry it, and the application starts on the old data.
func ApplyPending(ctx context.Context, dbPath string, now func() time.Time) *Result {
	l := layoutFor(dbPath)
	var pend Pending
	switch err := readJSON(filepath.Join(l.pending(), pendingFile), &pend); {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return finish(l, now, Pending{}, "", err)
	}
	staged := filepath.Join(l.pending(), stagedDatabase)
	if _, err := os.Stat(staged); errors.Is(err, os.ErrNotExist) {
		// A crash between the rename into place and the clean-up: it was applied.
		_ = os.RemoveAll(l.pending())
		return nil
	}
	safety, err := apply(ctx, l, dbPath, staged, pend, now)
	return finish(l, now, pend, safety, err)
}

func finish(l layout, now func() time.Time, pend Pending, safety string, err error) *Result {
	r := &Result{
		Status: StatusApplied, At: now().UTC().Format(time.RFC3339), SafetyCopy: safety,
		BundleCreatedAt: pend.Manifest.CreatedAt, BundleVersion: pend.Manifest.AppVersion,
	}
	if err != nil {
		r.Status, r.Error = StatusFailed, err.Error()
		slog.Error("importflow: applying the PC migration bundle failed; keeping the current database", "error", err, "safety_copy", safety)
	} else {
		slog.Info("importflow: applied the PC migration bundle", "safety_copy", safety, "bundle_created_at", r.BundleCreatedAt)
	}
	if werr := ensure(l.root); werr == nil {
		if werr = writeJSON(l.lastResult(), r); werr != nil {
			slog.Error("importflow: record the import result", "error", werr)
		}
	}
	if rerr := os.RemoveAll(l.pending()); rerr != nil {
		slog.Error("importflow: remove the applied import", "error", rerr)
	}
	return r
}

func apply(ctx context.Context, l layout, dbPath, staged string, pend Pending, now func() time.Time) (safety string, err error) {
	if err := migratebundle.VerifyDatabase(ctx, staged, pend.Manifest); err != nil {
		return "", fmt.Errorf("the staged bundle no longer verifies: %w", err)
	}
	if info, statErr := os.Stat(dbPath); statErr == nil && info.Size() > 0 {
		if safety, err = safetyCopy(ctx, l, dbPath, now()); err != nil {
			return "", err
		}
		if err := carryOver(ctx, staged, safety); err != nil {
			return safety, err
		}
	}
	if err := migrateStaged(ctx, staged); err != nil {
		return safety, err
	}
	// The old database's WAL/SHM belong to the file being replaced; they were
	// folded into it by the safety copy's checkpoint and must not meet the new file.
	for _, sfx := range []string{"-wal", "-shm"} {
		if err := os.Remove(dbPath + sfx); err != nil && !errors.Is(err, os.ErrNotExist) {
			return safety, fmt.Errorf("remove %s: %w", dbPath+sfx, err)
		}
	}
	if err := os.Rename(staged, dbPath); err != nil {
		return safety, fmt.Errorf("put the imported database in place: %w", err)
	}
	return safety, nil
}

// safetyCopy writes a verified copy of the current database to
// migration/safety/ so the replace is reversible (copy the file back over
// the database while the application is stopped). It reads through one
// VACUUM INTO snapshot after a WAL checkpoint (sqlitedb.BackupTo), like the
// daily backup, but keeps the secrets: it never leaves this PC.
func safetyCopy(ctx context.Context, l layout, dbPath string, at time.Time) (path string, err error) {
	if err := ensure(l.safety()); err != nil {
		return "", err
	}
	path = filepath.Join(l.safety(), "pitha-before-import-"+at.UTC().Format(safetyTimestamp)+".db")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
	if err != nil {
		return "", fmt.Errorf("create the safety copy: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("create the safety copy: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(path)
			path = ""
		}
	}()
	cur, err := sql.Open("sqlite", sqlitedb.FileURI(dbPath, "mode=rw&_busy_timeout=60000"))
	if err != nil {
		return "", fmt.Errorf("open the current database for the safety copy: %w", err)
	}
	defer func() { err = errors.Join(err, cur.Close()) }()
	cur.SetMaxOpenConns(1)
	if _, err := sqlitedb.BackupTo(ctx, cur, path); err != nil {
		return "", fmt.Errorf("safety copy of the current database: %w", err)
	}
	if err := backup.VerifyCopy(ctx, path); err != nil {
		return "", fmt.Errorf("safety copy of the current database: %w", err)
	}
	return path, nil
}
