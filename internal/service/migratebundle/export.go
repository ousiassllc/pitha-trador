package migratebundle

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/backup"
	"github.com/ousiassllc/pitha-trador/internal/version"
)

// Exporter builds bundles from the live database.
type Exporter struct {
	db  *sql.DB
	now func() time.Time
}

// NewExporter returns an Exporter over the application's database.
func NewExporter(db *sql.DB) *Exporter { return &Exporter{db: db, now: time.Now} }

// Export writes one bundle to w and returns its manifest. The database is
// copied as one consistent snapshot (sqlitedb.BackupTo: WAL checkpoint +
// VACUUM INTO) into a private temporary directory, so the application keeps
// writing meanwhile and a failure leaves nothing behind but an error. The
// copy is scrubbed (backup.CopyScrubbed + pruneSettings), integrity-checked,
// counted, hashed and only then packaged; w gets nothing before the copy
// passed every check.
func (e *Exporter) Export(ctx context.Context, w io.Writer) (Manifest, error) {
	dir, err := os.MkdirTemp("", "pitha-bundle-*")
	if err != nil {
		return Manifest{}, fmt.Errorf("migratebundle: create work directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	copyPath := filepath.Join(dir, DatabaseName)
	f, err := os.OpenFile(copyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Manifest{}, fmt.Errorf("migratebundle: create database copy: %w", err)
	}
	if err := f.Close(); err != nil {
		return Manifest{}, fmt.Errorf("migratebundle: create database copy: %w", err)
	}

	var excluded []string
	prune := func(ctx context.Context, conn *sql.DB) (err error) {
		excluded, err = pruneSettings(ctx, conn)
		return err
	}
	if err := backup.CopyScrubbed(ctx, e.db, copyPath, prune); err != nil {
		return Manifest{}, fmt.Errorf("migratebundle: copy database: %w", err)
	}

	m, err := describeCopy(ctx, copyPath)
	if err != nil {
		return Manifest{}, err
	}
	m.FormatVersion, m.AppVersion, m.Database = FormatVersion, version.Version, DatabaseName
	m.CreatedAt = e.now().UTC().Format(time.RFC3339)
	m.ExcludedSettings = slices.Compact(slices.Sorted(slices.Values(append(ExcludedSettingKeys(), excluded...))))

	if err := writeArchive(w, m, copyPath); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// describeCopy fills the manifest fields that come from the finished copy:
// schema version, per-table row counts, size and SHA-256.
func describeCopy(ctx context.Context, path string) (m Manifest, err error) {
	conn, err := backup.OpenCopy(path)
	if err != nil {
		return Manifest{}, err
	}
	defer func() { err = errors.Join(err, conn.Close()) }()

	schema, dirty, err := SchemaVersion(ctx, conn)
	if err != nil {
		return Manifest{}, err
	}
	if dirty {
		return Manifest{}, errors.New("migratebundle: the database is in the middle of a migration (dirty schema_migrations); not exporting it")
	}
	m.SchemaVersion = schema
	if m.Tables, err = CountTables(ctx, conn); err != nil {
		return Manifest{}, err
	}
	m.DatabaseSHA256, m.DatabaseBytes, err = hashFile(path)
	return m, err
}

func hashFile(path string) (sum string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("migratebundle: open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if size, err = io.Copy(h, f); err != nil {
		return "", 0, fmt.Errorf("migratebundle: hash %q: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// writeArchive writes manifest.json then the database copy into a ZIP on w.
func writeArchive(w io.Writer, m Manifest, dbPath string) error {
	zw := zip.NewWriter(w)
	mw, err := zw.Create(ManifestName)
	if err != nil {
		return fmt.Errorf("migratebundle: write archive: %w", err)
	}
	enc := json.NewEncoder(mw)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return fmt.Errorf("migratebundle: write manifest: %w", err)
	}
	dw, err := zw.CreateHeader(&zip.FileHeader{Name: DatabaseName, Method: zip.Deflate})
	if err != nil {
		return fmt.Errorf("migratebundle: write archive: %w", err)
	}
	in, err := os.Open(dbPath)
	if err != nil {
		return fmt.Errorf("migratebundle: open database copy: %w", err)
	}
	defer func() { _ = in.Close() }()
	if _, err := io.Copy(dw, in); err != nil {
		return fmt.Errorf("migratebundle: write database: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("migratebundle: finish archive: %w", err)
	}
	return nil
}
