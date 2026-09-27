package repository

import (
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
func dsn(path string) string {
	return fmt.Sprintf("file:%s?_foreign_keys=1&_journal_mode=WAL", path)
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
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("repository: create database directory %q: %w", dir, err)
		}
	}

	registerInstrumentedDriver()
	conn, err := sql.Open(instrumentedDriverName, dsn(path))
	if err != nil {
		return nil, fmt.Errorf("repository: open sqlite database %q: %w", path, err)
	}

	if err := conn.Ping(); err != nil {
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
