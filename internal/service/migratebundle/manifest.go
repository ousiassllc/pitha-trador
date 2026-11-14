package migratebundle

import (
	"errors"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// File layout and version of the bundle.
const (
	// FormatVersion is the bundle layout version. A reader accepts exactly the
	// version it was written for; the layout changes only with a new number.
	FormatVersion = 1
	// Extension is the file name extension of a bundle (a ZIP archive).
	Extension = ".pitha-bundle"
	// ManifestName and DatabaseName are the two entries of the archive.
	ManifestName = "manifest.json"
	DatabaseName = "pitha.db"
)

// TableCount is one table's row count. The table list is enumerated from
// sqlite_master, never hardcoded, so a table a later migration adds is
// counted without a code change.
type TableCount struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
}

// Manifest describes a bundle (manifest.json).
type Manifest struct {
	FormatVersion int `json:"format_version"`
	// AppVersion is the exporting build's version ("dev" for a branch build).
	AppVersion string `json:"app_version"`
	// SchemaVersion is the database's golang-migrate version. An import
	// refuses a bundle whose schema is newer than the running build's; an
	// older one is migrated forward when the database is opened.
	SchemaVersion uint `json:"schema_version"`
	// CreatedAt is the export time, UTC RFC 3339.
	CreatedAt      string       `json:"created_at"`
	Database       string       `json:"database"`
	DatabaseSHA256 string       `json:"database_sha256"`
	DatabaseBytes  int64        `json:"database_bytes"`
	Tables         []TableCount `json:"tables"`
	// SecretsIncluded is always false: there is no way to put secrets into a
	// bundle. The field makes that visible to whoever opens the file.
	SecretsIncluded bool `json:"secrets_included"`
	// ExcludedSettings are the runtime_settings keys left out of the file
	// (machine-specific paths, secret-bearing values); the importing PC keeps
	// its own values for them.
	ExcludedSettings []string `json:"excluded_settings"`
}

// Sentinel errors of reading a bundle; importflow turns them into messages.
var (
	// ErrNotBundle: the file is not a readable bundle (not a ZIP, no manifest or database entry, unparsable manifest).
	ErrNotBundle = errors.New("migratebundle: not a migration bundle")
	// ErrUnsupportedFormat: the bundle's format version is not FormatVersion.
	ErrUnsupportedFormat = errors.New("migratebundle: unsupported bundle format version")
	// ErrSchemaTooNew: the bundle's database schema is newer than this build's.
	ErrSchemaTooNew = errors.New("migratebundle: bundle schema is newer than this application")
	// ErrChecksum: the database entry does not match the manifest's size or SHA-256.
	ErrChecksum = errors.New("migratebundle: database does not match the manifest checksum")
	// ErrCorrupt: the database fails SQLite's integrity check or contradicts the manifest.
	ErrCorrupt = errors.New("migratebundle: bundle database is corrupt")
	// ErrNotSecretFree: the bundle carries secrets or machine-specific settings.
	ErrNotSecretFree = errors.New("migratebundle: bundle carries secrets or machine-specific settings")
)

// ExcludedSettingKeys are the runtime_settings keys a bundle never carries:
// the directories of this PC (backup folder, log folder - the new PC picks its
// own, and a path copied from the old one would point nowhere or, worse, at
// the wrong disk) and the 立花 秘密鍵 file paths (the key files themselves are
// never in the database and stay on the old PC; the new PC re-enters its
// path). The snapshot archive and default backup folders are not settings at
// all: they sit next to the database and are not part of it.
func ExcludedSettingKeys() []string {
	return []string{
		config.KeyBackupDir,
		config.KeyLogDir,
		config.KeyTachibanaDemoPrivateKeyPath,
		config.KeyTachibanaProdPrivateKeyPath,
	}
}

func isExcludedKey(key string) bool { return slices.Contains(ExcludedSettingKeys(), key) }
