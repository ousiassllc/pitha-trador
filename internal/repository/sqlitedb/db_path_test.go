package sqlitedb_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

// TestOpen_PathWithURISpecialCharacters pins issue #538: '#', '?' and '%'
// in the database path must reach SQLite as literal characters, so the
// file Open pre-creates at 0600 is the one actually used (not a truncated
// sibling) and its data survives a reopen.
func TestOpen_PathWithURISpecialCharacters(t *testing.T) {
	for _, dir := range []string{"a#b", "a?b", "a%41", "a b#c?d%e"} {
		t.Run(dir, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, dir, "x.db")

			conn, err := sqlitedb.Open(path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if _, err := conn.Exec(`CREATE TABLE path_probe (id INTEGER PRIMARY KEY)`); err != nil {
				t.Fatalf("create table: %v", err)
			}
			if _, err := conn.Exec(`INSERT INTO path_probe (id) VALUES (1)`); err != nil {
				t.Fatalf("insert: %v", err)
			}
			if err := conn.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}

			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat db file: %v", err)
			}
			if info.Size() == 0 {
				t.Fatalf("database file %q is empty: data went to a different file", path)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Errorf("db file mode = %o, want 600", got)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatalf("read root: %v", err)
			}
			if len(entries) != 1 || entries[0].Name() != dir {
				t.Errorf("stray files next to the database directory: %v", entries)
			}

			conn, err = sqlitedb.Open(path)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer func() { _ = conn.Close() }()
			var n int
			if err := conn.QueryRow(`SELECT count(*) FROM path_probe`).Scan(&n); err != nil || n != 1 {
				t.Fatalf("row count after reopen = %d, err = %v; want 1", n, err)
			}
		})
	}
}
