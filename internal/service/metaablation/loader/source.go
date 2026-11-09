// Package loader is the read-only data access of the metaablation CLI (issue
// #1046). It opens a COPY of the database with SQLite mode=ro
// (sqlitedb.OpenReadOnly: no migration, no creation, a write fails), reads the
// pinned model and the stored decisions, labels and market snapshots, and
// rebuilds the inputs of the label layer (eligibility.Input) and the replay
// layer from what is stored. Nothing here writes, trains, calibrates or calls
// an exchange or an LLM; a value that cannot be rebuilt from stored data is
// reported as unrecoverable (counted as "unknown" by the eligibility package),
// never made up.
package loader

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// Source is a read-only handle on a database copy.
type Source struct {
	db   *sql.DB
	path string
}

// Open opens the existing database file at path read-only. It is the only way
// the package reaches a database.
func Open(path string) (*Source, error) {
	db, err := sqlitedb.OpenReadOnly(path)
	if err != nil {
		return nil, err
	}
	return &Source{db: db, path: path}, nil
}

// Close releases the handle.
func (s *Source) Close() error { return s.db.Close() }

// DB is the read-only handle (mode=ro); a write through it fails.
func (s *Source) DB() *sql.DB { return s.db }

// Path is the file the Source was opened on.
func (s *Source) Path() string { return s.path }

// FileSHA256 is the SHA-256 (hex) of the file's content: the identifier of a
// database copy recorded in the manifest and checked before and after a run.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("loader: open %s for hashing: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("loader: hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
