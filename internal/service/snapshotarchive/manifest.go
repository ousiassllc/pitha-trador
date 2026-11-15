package snapshotarchive

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// manifestVersion is the manifest.json format version.
const manifestVersion = 1

// manifestName is the index file of the archive, next to the year folders.
const manifestName = "manifest.json"

// Day is one archived UTC date.
type Day struct {
	Date           string `json:"date"`
	Rows           int    `json:"rows"`
	FirstTimestamp string `json:"first_timestamp"`
	LastTimestamp  string `json:"last_timestamp"`
	SHA256         string `json:"sha256"`
	// File is the path of the archive file relative to the archive's table
	// folder (forward slashes).
	File       string `json:"file"`
	ArchivedAt string `json:"archived_at"`
	// Action is how this run reached the verified file: "written" (new),
	// "rewritten" (re-archived because rows arrived after an earlier
	// archive) or "verified" (an existing file already covered the day). It
	// is not stored in the manifest.
	Action string `json:"-"`
}

// Manifest is the index of the archived days: a convenience for people and
// tools (the files are the data and are re-verified on every run).
type Manifest struct {
	Version int    `json:"version"`
	Table   string `json:"table"`
	Days    []Day  `json:"days"`
}

// ReadManifest reads the manifest of the archive in dir. A missing manifest
// is an empty one; an unparsable one is an error.
func ReadManifest(dir string) (Manifest, error) {
	path := filepath.Join(dir, Table, manifestName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{Version: manifestVersion, Table: Table}, nil
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("snapshotarchive: read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, fmt.Errorf("snapshotarchive: parse manifest %s: %w", path, err)
	}
	if m.Version != manifestVersion {
		return Manifest{}, fmt.Errorf("snapshotarchive: manifest %s has unsupported version %d", path, m.Version)
	}
	return m, nil
}

// upsert replaces the entry of d.Date or adds it, keeping Days ordered by
// date. It reports whether the manifest changed.
func (m *Manifest) upsert(d Day) bool {
	i, found := slices.BinarySearchFunc(m.Days, d.Date, func(e Day, date string) int {
		return strings.Compare(e.Date, date)
	})
	if found {
		if m.Days[i] == (Day{
			Date: d.Date, Rows: d.Rows, FirstTimestamp: d.FirstTimestamp, LastTimestamp: d.LastTimestamp,
			SHA256: d.SHA256, File: d.File, ArchivedAt: m.Days[i].ArchivedAt,
		}) {
			return false
		}
		m.Days[i] = d
		return true
	}
	m.Days = slices.Insert(m.Days, i, d)
	return true
}

// find returns the entry of date.
func (m Manifest) find(date string) (Day, bool) {
	for _, d := range m.Days {
		if d.Date == date {
			return d, true
		}
	}
	return Day{}, false
}

// writeManifest stores m atomically (temporary file, fsync, rename).
func (a *Archiver) writeManifest(m Manifest) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(a.tableDir(), manifestName), append(raw, '\n'))
}

// writeFileAtomic writes data to path through a temporary file in the same
// folder, fsyncs it and renames it over path, so a crash never leaves a
// partial file under path.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	return syncDir(dir)
}

// syncDir fsyncs a folder so a rename in it survives a crash. Windows
// cannot open a folder for fsync, so a failure to open it is not an error.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}
	return nil
}
