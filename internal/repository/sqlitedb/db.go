package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"     // registers the "sqlite" database/sql driver (pure Go, CGO-free)
	_ "modernc.org/sqlite/vec" // registers the vec0 virtual table module (sqlite-vec, docs/architecture/er.md §ベクトルインデックス) via sqlite3_auto_extension, for db/migrations' CREATE VIRTUAL TABLE ... USING vec0 statements and internal/service/rag's queries

	"github.com/ousiassllc/pitha-trador/db"
)

// dsn builds the modernc.org/sqlite DSN for the database file at path.
//
// `_foreign_keys=1` and `_journal_mode=WAL` are shorthand DSN keys that
// modernc.org/sqlite re-applies as `PRAGMA foreign_keys=ON` /
// `PRAGMA journal_mode=WAL` every time database/sql opens a new pooled
// connection, not just the first one (docs/architecture/er.md §型・規約
// 「外部キー」「同時実行」: "Goのコネクションプール初期化時に必ず設定する").
//
// `_busy_timeout=60000` maps to `PRAGMA busy_timeout=60000` (60s). WAL mode
// allows one writer to run concurrently with readers, but a second writer
// (e.g. another pooled connection from the same *sql.DB, or a separate
// process) that finds the write lock already held returns SQLITE_BUSY
// immediately when busy_timeout is unset (its default is 0). Setting a
// timeout makes SQLite retry internally instead of failing fast. The
// timeout only bounds how long a contended writer is willing to wait for
// the lock to free up; it adds zero latency when there is no contention,
// so a generous value here has no cost on the uncontended (production)
// path. This started at 5s, then 30s, and is now 60s: under `go test
// ./...`, every package's test binary runs concurrently, and on a
// sufficiently CPU-contended host (e.g. many unrelated processes also
// competing for the same cores) that contention can stretch a writer's
// actual hold time far beyond what it takes when that writer runs alone.
// Observed on a heavily loaded host: a lone run of the concurrent-writer
// regression test finishes in ~4s at the median but tailed out to 66s
// under full-suite parallelism at 30s busy_timeout, so the regression
// test's own writer concurrency was also reduced (see
// busy_timeout_test.go) alongside this bump, to keep the serialized
// write queue bounded instead of chasing CPU contention with an
// unbounded timeout.
//
// `_txlock=immediate` makes every non-read-only `BeginTx` issue `BEGIN
// IMMEDIATE` instead of the default deferred `BEGIN`. Repositories such
// as JobRepository.ClaimNext and SnapshotRepository run a SELECT followed
// by an UPDATE/INSERT inside one transaction; a deferred transaction only
// acquires the write lock at that later write statement, so two
// concurrent transactions can both finish their SELECT and then race for
// the upgrade to a write lock. That race returns SQLITE_BUSY even with
// busy_timeout set, because it is a lock-upgrade conflict rather than a
// plain "wait for the writer to finish" wait. Acquiring the write lock
// immediately at BEGIN serializes those transactions through the normal
// busy_timeout retry path instead. Together, `_busy_timeout` and
// `_txlock=immediate` eliminate the intermittent "database is locked (5)
// (SQLITE_BUSY)" failures seen under concurrent writers (issue #39).
func dsn(path string) string {
	return fmt.Sprintf("file:%s?_foreign_keys=1&_journal_mode=WAL&_busy_timeout=60000&_txlock=immediate", path)
}

const (
	dirMode  os.FileMode = 0o700
	fileMode os.FileMode = 0o600
)

// ensurePrivateFile creates the database file with 0600 if it does not
// exist yet and narrows an existing one to 0600 (e.g. a 0644 file left by
// an older version). SQLite then derives the permissions of its -wal/-shm
// files from this file.
func ensurePrivateFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return fmt.Errorf("repository: create database file %q: %w", path, err)
	}
	if err := f.Chmod(fileMode); err != nil {
		return errors.Join(fmt.Errorf("repository: restrict database file %q: %w", path, err), f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("repository: close database file %q: %w", path, err)
	}
	return nil
}

// Open opens (creating it and its parent directory if necessary) the
// SQLite database file at path, applies every pending golang-migrate
// migration embedded in db/migrations, and returns a *sql.DB ready for use
// by repositories.
//
// Every connection returned from the pool has foreign key enforcement and
// WAL journaling enabled (docs/architecture/overview.md §10.1: "マイグレー
// ション適用確認（golang-migrate）・接続初期化（PRAGMA foreign_keys=ON,
// WAL）"). Every query/exec issued through it is also logged (query text,
// duration, error) via dbmw.go's sqlmw-wrapped driver
// (requirements/non-functional.md §5.1 "DB latency / エラー"). Callers are
// responsible for closing the returned *sql.DB.
//
// The database holds the secrets table (broker API password etc.,
// encrypted only with an in-binary key), so a newly created parent
// directory is 0700 and the database file is pre-created/forced to 0600
// before SQLite opens it; SQLite gives the -wal/-shm/-journal side files
// the main file's mode. A pre-existing parent directory is left untouched
// because PITHA_DB_PATH may point into a shared directory (issue #334).
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, dirMode); err != nil {
			return nil, fmt.Errorf("repository: create database directory %q: %w", dir, err)
		}
	}
	if err := ensurePrivateFile(path); err != nil {
		return nil, err
	}

	registerInstrumentedDriver()
	conn, err := sql.Open(instrumentedDriverName, dsn(path))
	if err != nil {
		return nil, fmt.Errorf("repository: open sqlite database %q: %w", path, err)
	}

	if err := conn.PingContext(context.Background()); err != nil {
		return nil, errors.Join(
			fmt.Errorf("repository: connect to sqlite database %q: %w", path, err),
			conn.Close(),
		)
	}

	if err := migrateUp(conn); err != nil {
		return nil, errors.Join(err, conn.Close())
	}

	return conn, nil
}

// migrateUp applies every migration embedded under db/migrations to conn
// via golang-migrate. It is a no-op (not an error) if the schema is already
// current.
func migrateUp(conn *sql.DB) error {
	sourceDriver, err := iofs.New(db.MigrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("repository: load embedded migrations: %w", err)
	}

	dbDriver, err := migratesqlite.WithInstance(conn, &migratesqlite.Config{})
	if err != nil {
		return fmt.Errorf("repository: init migrate database driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite", dbDriver)
	if err != nil {
		return fmt.Errorf("repository: init migrate instance: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("repository: apply migrations: %w", err)
	}

	return nil
}

// BackupTo writes a transactionally consistent copy of the database
// behind conn to destPath (docs/requirements/non-functional.md §3 "DB
// バックアップ"). It first runs `PRAGMA wal_checkpoint(TRUNCATE)` so the
// WAL's contents are folded into the main database file and the -wal
// file is emptied, then `VACUUM INTO` produces the copy. A plain file
// copy of the .db file would be torn if another pooled connection wrote
// or auto-checkpointed mid-copy; VACUUM INTO reads a single snapshot and
// so is safe while the application keeps writing. destPath MUST NOT
// already hold data (SQLite refuses to overwrite a non-empty file; an
// empty file is accepted, which lets the caller pre-create it with
// restrictive permissions) and its parent directory MUST exist.
//
// A checkpoint that cannot fully complete (busy != 0: a concurrent
// reader/writer blocked the TRUNCATE) is not an error - the snapshot
// taken by VACUUM INTO remains consistent - but is reported via the
// returned checkpointBusy flag so the caller can log it.
func BackupTo(ctx context.Context, conn *sql.DB, destPath string) (checkpointBusy bool, err error) {
	var busy, walFrames, checkpointed int
	if err := conn.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &walFrames, &checkpointed); err != nil {
		return false, fmt.Errorf("repository: wal checkpoint before backup: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", destPath); err != nil {
		return busy != 0, fmt.Errorf("repository: vacuum into %q: %w", destPath, err)
	}
	return busy != 0, nil
}
