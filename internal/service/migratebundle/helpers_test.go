package migratebundle_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func sha(b []byte) (string, int64) {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), int64(len(b))
}

// unscrubbedCopy returns a VACUUM INTO copy of conn that still has its secrets.
func unscrubbedCopy(t *testing.T, conn *sql.DB) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "raw.db")
	if _, err := conn.Exec("VACUUM INTO ?", path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
