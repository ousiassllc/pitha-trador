package evalfixture

import (
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// The migrations are the slow part of a test under -race (about two seconds each), so a
// seeded database is built once per test binary and kept as the bytes of a cleanly closed
// SQLite file; every test gets its own copy, opened with the production sqlitedb.Open (the
// schema is current, so its migrations are a no-op).
var (
	templatesMu sync.Mutex
	templates   = map[string]*template{}
	base        template
)

type template struct {
	once sync.Once
	raw  []byte
	err  error
}

// buildFrom opens a copy of from (nil: an empty file, so Open migrates), lets seed fill it and returns the closed file's bytes.
func buildFrom(from []byte, seed func(db *sql.DB)) ([]byte, error) {
	dir, err := os.MkdirTemp("", "evalfixture-template-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "pitha.db")
	if from != nil {
		if err := os.WriteFile(path, from, 0o600); err != nil {
			return nil, err
		}
	}
	db, err := sqlitedb.Open(path)
	if err != nil {
		return nil, err
	}
	seed(db)
	if err := db.Close(); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// OpenSeeded returns a fresh migrated database (in the test's temp dir, opened with
// sqlitedb.Open) holding what seed put into it. seed runs once per key and test binary.
func OpenSeeded(t *testing.T, key string, seed func(db *sql.DB)) *sql.DB {
	t.Helper()
	base.once.Do(func() { base.raw, base.err = buildFrom(nil, func(*sql.DB) {}) })
	if base.err != nil {
		t.Fatalf("build fixture: %v", base.err)
	}
	templatesMu.Lock()
	tpl, ok := templates[key]
	if !ok {
		tpl = &template{}
		templates[key] = tpl
	}
	templatesMu.Unlock()
	tpl.once.Do(func() { tpl.raw, tpl.err = buildFrom(base.raw, seed) })
	if tpl.err != nil {
		t.Fatalf("build fixture %s: %v", key, tpl.err)
	}
	path := filepath.Join(t.TempDir(), "pitha.db")
	if err := os.WriteFile(path, tpl.raw, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
