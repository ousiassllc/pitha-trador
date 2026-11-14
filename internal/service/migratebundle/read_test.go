package migratebundle_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/bundletest"
)

// rebuild re-zips bundle with an edited manifest (and database bytes), the way
// a damaged or hand-edited file would look.
func rebuild(t *testing.T, bundle []byte, edit func(*migratebundle.Manifest), editDB func([]byte) []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case f.Name == migratebundle.ManifestName && edit != nil:
			var m migratebundle.Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			edit(&m)
			if data, err = json.Marshal(m); err != nil {
				t.Fatal(err)
			}
		case f.Name == migratebundle.DatabaseName && editDB != nil:
			data = editDB(data)
		}
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func rewriteManifest(t *testing.T, bundle []byte, _ migratebundle.Manifest, edit func(*migratebundle.Manifest)) []byte {
	return rebuild(t, bundle, edit, nil)
}

func TestUnpack_RefusesDamagedBundles(t *testing.T) {
	conn, _ := bundletest.OpenDB(t)
	bundletest.Seed(t, conn)
	bundle, _ := export(t, migratebundle.NewExporter(conn))

	flip := func(b []byte) []byte {
		out := append([]byte(nil), b...)
		out[len(out)/2] ^= 0xFF
		return out
	}
	rehash := func(m *migratebundle.Manifest, b []byte) {
		// A tamperer who also fixes the checksum is caught by the integrity /
		// content checks instead.
		sum, size := sha(b)
		m.DatabaseSHA256, m.DatabaseBytes = sum, size
	}
	tests := map[string]struct {
		bundle []byte
		want   error
	}{
		"not a zip":         {[]byte("this is not a bundle"), migratebundle.ErrNotBundle},
		"flipped byte":      {rebuild(t, bundle, nil, flip), migratebundle.ErrChecksum},
		"truncated db":      {rebuild(t, bundle, nil, func(b []byte) []byte { return b[:len(b)/2] }), migratebundle.ErrChecksum},
		"wrong row counts":  {rebuild(t, bundle, func(m *migratebundle.Manifest) { m.Tables[0].Rows += 7 }, nil), migratebundle.ErrCorrupt},
		"wrong schema":      {rebuild(t, bundle, func(m *migratebundle.Manifest) { m.SchemaVersion-- }, nil), migratebundle.ErrCorrupt},
		"garbage database":  {rebuild(t, bundle, func(m *migratebundle.Manifest) { rehash(m, []byte("garbage")) }, func([]byte) []byte { return []byte("garbage") }), migratebundle.ErrCorrupt},
		"checksum is wrong": {rebuild(t, bundle, func(m *migratebundle.Manifest) { m.DatabaseSHA256 = "00" }, nil), migratebundle.ErrChecksum},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := unpack(t, tc.bundle); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// A bundle someone else made that carries a secrets row is refused even when
// its manifest and checksum are consistent.
func TestUnpack_RefusesBundleCarryingSecrets(t *testing.T) {
	conn, _ := bundletest.OpenDB(t)
	bundletest.Seed(t, conn)
	// Build a bundle from a database whose secrets were NOT scrubbed, by
	// exporting and then swapping in an unscrubbed copy with a matching manifest.
	raw := unscrubbedCopy(t, conn)
	bundle, _ := export(t, migratebundle.NewExporter(conn))
	forged := rebuild(t, bundle, func(m *migratebundle.Manifest) {
		sum, size := sha(raw)
		m.DatabaseSHA256, m.DatabaseBytes = sum, size
		for i := range m.Tables {
			if m.Tables[i].Name == "secrets" {
				m.Tables[i].Rows = 2
			}
			if m.Tables[i].Name == "runtime_settings" {
				m.Tables[i].Rows = 5
			}
		}
	}, func([]byte) []byte { return raw })
	if _, _, err := unpack(t, forged); !errors.Is(err, migratebundle.ErrNotSecretFree) {
		t.Fatalf("err = %v, want ErrNotSecretFree", err)
	}
}

// A bundle from an older release (schema behind this build's) is accepted:
// the normal open migrates it forward.
func TestUnpack_AcceptsOlderSchema(t *testing.T) {
	latest, err := migratebundle.LatestSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	old, _ := bundletest.OpenDBAtVersion(t, latest-1)
	bundle, m := export(t, migratebundle.NewExporter(old))
	if m.SchemaVersion != latest-1 {
		t.Fatalf("schema = %d, want %d", m.SchemaVersion, latest-1)
	}
	if _, got, err := unpack(t, bundle); err != nil || got.SchemaVersion != latest-1 {
		t.Fatalf("Unpack: manifest %+v, err %v", got, err)
	}
}

func TestExport_ReportsCanceledContext(t *testing.T) {
	conn, _ := bundletest.OpenDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := migratebundle.NewExporter(conn).Export(ctx, io.Discard); err == nil {
		t.Fatal("Export with a canceled context succeeded")
	}
}
