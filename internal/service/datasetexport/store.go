package datasetexport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// exportsDir is <dir of the database file>/exports. The database file is found through the
// read-only connection itself, so no argument and no setting chooses where the files go.
func exportsDir(ctx context.Context, q query.Q) (string, error) {
	var file string
	if err := q.QueryRowContext(ctx, `SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&file); err != nil {
		return "", fmt.Errorf("datasetexport: find the database file: %w", err)
	}
	if file == "" {
		return "", errors.New("datasetexport: the database has no file (in-memory): nowhere to export to")
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", fmt.Errorf("datasetexport: database path: %w", err)
	}
	return filepath.Join(filepath.Dir(abs), ExportsDirName), nil
}

// outcome of publishing a dataset.
type outcome int

const (
	published outcome = iota // the folder was created
	identical                // the folder already holds the same files: left as it is
	conflict                 // the folder exists with other content: never overwritten
)

// publish writes the dataset folder <base>/<id>: the files go into a fresh temporary folder
// (0700, files 0600, synced) beside it, which is then renamed into place, so a reader never
// sees half a dataset. A folder that already exists is never touched: if its files hash the
// same as the new ones it is the same dataset (the id is the content's hash) and its own
// manifest is returned; otherwise it is a conflict.
func publish(base, id string, files []encoded, manifest Manifest) (outcome, Manifest, error) {
	final := filepath.Join(base, id)
	if err := os.MkdirAll(base, DirMode); err != nil {
		return 0, Manifest{}, fmt.Errorf("datasetexport: create exports folder: %w", err)
	}
	if _, err := os.Lstat(final); err == nil {
		return verifyExisting(final, files, manifest)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return 0, Manifest{}, fmt.Errorf("datasetexport: inspect dataset folder: %w", err)
	}

	tmp, err := os.MkdirTemp(base, ".tmp-"+id+"-")
	if err != nil {
		return 0, Manifest{}, fmt.Errorf("datasetexport: create temporary folder: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }() // a no-op once renamed
	if err := os.Chmod(tmp, DirMode); err != nil {
		return 0, Manifest{}, fmt.Errorf("datasetexport: set folder mode: %w", err)
	}
	for _, f := range files {
		if err := writeFile(filepath.Join(tmp, f.name), f.data); err != nil {
			return 0, Manifest{}, err
		}
	}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return 0, Manifest{}, fmt.Errorf("datasetexport: encode manifest: %w", err)
	}
	if err := writeFile(filepath.Join(tmp, ManifestName), append(body, '\n')); err != nil {
		return 0, Manifest{}, err
	}
	if err := os.Rename(tmp, final); err != nil {
		if _, serr := os.Lstat(final); serr == nil { // another call published the same id meanwhile
			return verifyExisting(final, files, manifest)
		}
		return 0, Manifest{}, fmt.Errorf("datasetexport: publish dataset folder: %w", err)
	}
	return published, manifest, nil
}

// writeFile creates a new file with mode 0600 (it never replaces one), writes and syncs it.
func writeFile(path string, data []byte) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, FileMode)
	if err != nil {
		return fmt.Errorf("datasetexport: create %s: %w", filepath.Base(path), err)
	}
	defer func() {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("datasetexport: close %s: %w", filepath.Base(path), cerr)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("datasetexport: write %s: %w", filepath.Base(path), err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("datasetexport: sync %s: %w", filepath.Base(path), err)
	}
	return nil
}

// verifyExisting compares the existing folder with the dataset about to be written: every data
// file must hash as the new one and manifest.json must parse and carry the same dataset hash.
// Anything else (a missing or different file, a symlink, a bad manifest) is a conflict.
func verifyExisting(dir string, files []encoded, manifest Manifest) (outcome, Manifest, error) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return conflict, manifest, nil
	}
	for _, f := range files {
		sum, err := hashFile(filepath.Join(dir, f.name))
		if err != nil || sum != f.sha256() {
			return conflict, manifest, nil
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, ManifestName)) //nolint:gosec // G304: dir is our own derived exports folder
	if err != nil {
		return conflict, manifest, nil
	}
	var existing Manifest
	if json.Unmarshal(raw, &existing) != nil || existing.DatasetSHA256 != manifest.DatasetSHA256 {
		return conflict, manifest, nil
	}
	return identical, existing, nil
}

func hashFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	b, err := os.ReadFile(path) //nolint:gosec // G304: path is inside our own derived exports folder
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
