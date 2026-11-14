package importflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Everything lives in one folder next to the database, so the staged file is
// on the same filesystem as the database (atomic rename) and one folder holds
// every trace of an import.
const (
	dirName         = "migration"
	stagingName     = "staging"
	pendingName     = "pending"
	safetyName      = "safety"
	previewFile     = "preview.json"
	pendingFile     = "pending.json"
	lastResultFile  = "last-result.json"
	stagedDatabase  = "pitha.db"
	uploadFile      = "upload.pitha-bundle"
	dirMode         = os.FileMode(0o700)
	fileMode        = os.FileMode(0o600)
	safetyTimestamp = "20060102-150405"
)

var tokenRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

type layout struct{ root string }

func layoutFor(dbPath string) layout { return layout{filepath.Join(filepath.Dir(dbPath), dirName)} }

func (l layout) staging() string                { return filepath.Join(l.root, stagingName) }
func (l layout) stagingDir(token string) string { return filepath.Join(l.staging(), token) }
func (l layout) pending() string                { return filepath.Join(l.root, pendingName) }
func (l layout) safety() string                 { return filepath.Join(l.root, safetyName) }
func (l layout) lastResult() string             { return filepath.Join(l.root, lastResultFile) }

// ensure creates dir (0700) and any missing parent.
func ensure(dir string) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("importflow: create %q: %w", dir, err)
	}
	return nil
}
