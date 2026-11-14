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
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/service/backup"
)

// maxManifestBytes bounds the manifest entry read into memory.
const maxManifestBytes = 1 << 20

// ReadManifest parses and validates the manifest of the bundle in src, and
// checks the version compatibility, without extracting anything: the format
// version must be FormatVersion (ErrUnsupportedFormat) and the database
// schema must not be newer than this build's (ErrSchemaTooNew; an older one
// is accepted and migrated forward when the database is opened). A non-ZIP
// file, other entries than manifest.json and pitha.db, or an unparsable
// manifest is ErrNotBundle.
func ReadManifest(src io.ReaderAt, size int64) (Manifest, *zip.Reader, error) {
	zr, err := zip.NewReader(src, size)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("%w: %v", ErrNotBundle, err)
	}
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{ManifestName, DatabaseName}) {
		return Manifest{}, nil, fmt.Errorf("%w: unexpected archive entries %q", ErrNotBundle, names)
	}
	mf, err := findEntry(zr, ManifestName).Open()
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("%w: %v", ErrNotBundle, err)
	}
	defer func() { _ = mf.Close() }()
	raw, err := io.ReadAll(io.LimitReader(mf, maxManifestBytes+1))
	if err != nil || len(raw) > maxManifestBytes {
		return Manifest{}, nil, fmt.Errorf("%w: unreadable or oversized manifest", ErrNotBundle)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, nil, fmt.Errorf("%w: manifest: %v", ErrNotBundle, err)
	}
	if m.FormatVersion != FormatVersion {
		return Manifest{}, nil, fmt.Errorf("%w: bundle is format %d, this application reads format %d", ErrUnsupportedFormat, m.FormatVersion, FormatVersion)
	}
	latest, err := LatestSchemaVersion()
	if err != nil {
		return Manifest{}, nil, err
	}
	if m.SchemaVersion > latest {
		return Manifest{}, nil, fmt.Errorf("%w: bundle schema %d, this application's %d (update the application first)", ErrSchemaTooNew, m.SchemaVersion, latest)
	}
	if m.Database != DatabaseName || m.DatabaseBytes <= 0 || m.DatabaseSHA256 == "" {
		return Manifest{}, nil, fmt.Errorf("%w: manifest does not describe the database entry", ErrNotBundle)
	}
	return m, zr, nil
}

func findEntry(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// Unpack validates the bundle in src (ReadManifest), extracts its database to
// dst (a path that must not exist yet) and verifies it against the
// manifest: size and SHA-256 (ErrChecksum), SQLite's integrity check, the
// recorded schema version and per-table row counts (ErrCorrupt) and that it
// carries no secrets (ErrNotSecretFree). Nothing but dst is written, and dst
// is removed again when any check fails.
func Unpack(ctx context.Context, src io.ReaderAt, size int64, dst string) (m Manifest, err error) {
	m, zr, err := ReadManifest(src, size)
	if err != nil {
		return Manifest{}, err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Manifest{}, fmt.Errorf("migratebundle: create %q: %w", dst, err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(dst)
		}
	}()
	if err := extractDatabase(findEntry(zr, DatabaseName), out, m); err != nil {
		_ = out.Close()
		return Manifest{}, err
	}
	if err := out.Close(); err != nil {
		return Manifest{}, fmt.Errorf("migratebundle: write %q: %w", dst, err)
	}
	if err := VerifyDatabase(ctx, dst, m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// extractDatabase copies the database entry to out, refusing more or fewer
// bytes than the manifest declares or a different SHA-256.
func extractDatabase(entry *zip.File, out io.Writer, m Manifest) error {
	rc, err := entry.Open()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotBundle, err)
	}
	defer func() { _ = rc.Close() }()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(rc, m.DatabaseBytes+1))
	if err != nil {
		return fmt.Errorf("%w: read database: %v", ErrChecksum, err)
	}
	if n != m.DatabaseBytes {
		return fmt.Errorf("%w: database is %d bytes, manifest says %d", ErrChecksum, n, m.DatabaseBytes)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != m.DatabaseSHA256 {
		return fmt.Errorf("%w: SHA-256 %s, manifest says %s", ErrChecksum, got, m.DatabaseSHA256)
	}
	return nil
}

// VerifyDatabase checks the extracted database at path against m the way
// Unpack does after extraction; the startup apply repeats it on the staged
// file just before it is swapped in. ErrCorrupt / ErrNotSecretFree on failure.
func VerifyDatabase(ctx context.Context, path string, m Manifest) (err error) {
	got, _, err := hashFile(path)
	if err != nil {
		return err
	}
	if got != m.DatabaseSHA256 {
		return fmt.Errorf("%w: SHA-256 %s, manifest says %s", ErrChecksum, got, m.DatabaseSHA256)
	}
	if err := backup.VerifyCopy(ctx, path); err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	conn, err := backup.OpenCopy(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	defer func() { err = errors.Join(err, conn.Close()) }()
	return verifyContents(ctx, conn, m)
}

func verifyContents(ctx context.Context, conn *sql.DB, m Manifest) error {
	schema, dirty, err := SchemaVersion(ctx, conn)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if dirty || schema != m.SchemaVersion {
		return fmt.Errorf("%w: schema version %d (dirty=%t), manifest says %d", ErrCorrupt, schema, dirty, m.SchemaVersion)
	}
	tables, err := CountTables(ctx, conn)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if !slices.Equal(tables, m.Tables) {
		return fmt.Errorf("%w: table row counts differ from the manifest", ErrCorrupt)
	}
	return checkSecretFree(ctx, conn)
}
