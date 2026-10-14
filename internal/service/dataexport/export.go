package dataexport

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// SchemaVersion is the manifest / table-layout version. Bump it when a
// table or column is renamed, removed or re-typed; adding a column or table
// is backward compatible and keeps the version.
const SchemaVersion = 1

// ManifestFile is the archive entry that describes the export.
const ManifestFile = "manifest.json"

// ErrInvalidRequest is returned (wrapped) by Export for a request that can
// never succeed: no kinds, an unknown kind or format, or an empty period.
var ErrInvalidRequest = errors.New("dataexport: invalid request")

// Request selects what Export writes.
type Request struct {
	Kinds  []Kind
	Period Period
	Format Format
}

// Service writes export archives from its registered collectors.
type Service struct {
	collectors map[Kind]Collector
	now        func() time.Time
}

// New returns a Service over collectors; a later collector of the same Kind
// replaces an earlier one.
func New(collectors ...Collector) *Service {
	s := &Service{collectors: make(map[Kind]Collector, len(collectors)), now: time.Now}
	for _, c := range collectors {
		s.collectors[c.Kind()] = c
	}
	return s
}

// Export writes the ZIP archive for req to w and returns the manifest it
// wrote as manifest.json (the archive's last entry, after the row counts are
// known). Each table is a "<kind>/<table>.<format>" entry. Rows are streamed
// from the database, so memory use does not grow with the period.
//
// On error w may hold a partial archive; the caller must discard it.
func (s *Service) Export(ctx context.Context, w io.Writer, req Request) (Manifest, error) {
	collectors, err := s.resolve(req)
	if err != nil {
		return Manifest{}, err
	}

	generatedAt := s.now().UTC()
	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   generatedAt,
		Format:        req.Format,
		Period:        ManifestPeriod{From: req.Period.From.UTC(), To: req.Period.To.UTC()},
	}

	zw := zip.NewWriter(w)
	for _, c := range collectors {
		km := KindManifest{Kind: c.Kind()}
		for i, t := range c.Tables() {
			fm, err := writeTable(ctx, zw, c.Kind(), t, req, generatedAt)
			if err != nil {
				return Manifest{}, err
			}
			if i == 0 {
				km.Rows = fm.Rows
			}
			km.Files = append(km.Files, fm)
		}
		manifest.Kinds = append(manifest.Kinds, km)
	}

	if err := writeManifest(zw, manifest, generatedAt); err != nil {
		return Manifest{}, err
	}
	if err := zw.Close(); err != nil {
		return Manifest{}, fmt.Errorf("dataexport: finish archive: %w", err)
	}
	return manifest, nil
}

// resolve validates req and returns its collectors in request order.
func (s *Service) resolve(req Request) ([]Collector, error) {
	if !req.Format.valid() {
		return nil, fmt.Errorf("%w: format %q (want %q or %q)", ErrInvalidRequest, req.Format, FormatCSV, FormatJSONL)
	}
	if !req.Period.From.Before(req.Period.To) {
		return nil, fmt.Errorf("%w: period [%s, %s) is empty", ErrInvalidRequest, req.Period.From, req.Period.To)
	}
	if len(req.Kinds) == 0 {
		return nil, fmt.Errorf("%w: no kind selected", ErrInvalidRequest)
	}

	seen := make(map[Kind]bool, len(req.Kinds))
	out := make([]Collector, 0, len(req.Kinds))
	for _, k := range req.Kinds {
		c, ok := s.collectors[k]
		if !ok {
			return nil, fmt.Errorf("%w: unknown kind %q", ErrInvalidRequest, k)
		}
		if seen[k] {
			return nil, fmt.Errorf("%w: kind %q selected twice", ErrInvalidRequest, k)
		}
		seen[k] = true
		out = append(out, c)
	}
	return out, nil
}

// writeTable streams one table into its own archive entry.
func writeTable(ctx context.Context, zw *zip.Writer, kind Kind, t Table, req Request, modified time.Time) (FileManifest, error) {
	path := fmt.Sprintf("%s/%s.%s", kind, t.Name, req.Format)
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: path, Method: zip.Deflate, Modified: modified})
	if err != nil {
		return FileManifest{}, fmt.Errorf("dataexport: create %s: %w", path, err)
	}
	enc, err := newEncoder(req.Format, fw, t.Columns)
	if err != nil {
		return FileManifest{}, err
	}

	var rows int64
	err = t.Read(ctx, req.Period, func(row Row) error {
		prepared, err := prepareRow(t.Columns, row)
		if err != nil {
			return err
		}
		if err := enc.write(prepared); err != nil {
			return fmt.Errorf("dataexport: write %s: %w", path, err)
		}
		rows++
		return nil
	})
	if err != nil {
		return FileManifest{}, fmt.Errorf("dataexport: read %s/%s: %w", kind, t.Name, err)
	}
	if err := enc.flush(); err != nil {
		return FileManifest{}, fmt.Errorf("dataexport: flush %s: %w", path, err)
	}

	names := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		names[i] = c.Name
	}
	return FileManifest{Name: t.Name, Path: path, Rows: rows, Columns: names}, nil
}
