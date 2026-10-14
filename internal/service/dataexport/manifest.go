package dataexport

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"time"
)

// Manifest is the content of manifest.json.
type Manifest struct {
	SchemaVersion int            `json:"schema_version"`
	GeneratedAt   time.Time      `json:"generated_at"`
	Format        Format         `json:"format"`
	Period        ManifestPeriod `json:"period"`
	Kinds         []KindManifest `json:"kinds"`
}

// ManifestPeriod is the half-open range [From, To) (UTC) the export covers.
type ManifestPeriod struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// KindManifest describes one exported kind. Rows is the row count of its
// primary table (its first file): trades = orders, decisions = decisions.
type KindManifest struct {
	Kind  Kind           `json:"kind"`
	Rows  int64          `json:"rows"`
	Files []FileManifest `json:"files"`
}

// FileManifest describes one table file of the archive.
type FileManifest struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Rows    int64    `json:"rows"`
	Columns []string `json:"columns"`
}

func writeManifest(zw *zip.Writer, m Manifest, modified time.Time) error {
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: ManifestFile, Method: zip.Deflate, Modified: modified})
	if err != nil {
		return fmt.Errorf("dataexport: create %s: %w", ManifestFile, err)
	}
	enc := json.NewEncoder(fw)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return fmt.Errorf("dataexport: write %s: %w", ManifestFile, err)
	}
	return nil
}
