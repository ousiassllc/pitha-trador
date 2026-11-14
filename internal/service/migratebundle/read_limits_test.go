package migratebundle_test

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
)

// deflated returns n zero bytes deflate-compressed: a few KB that expand to n.
func deflated(t *testing.T, n int) []byte {
	t.Helper()
	var buf bytes.Buffer
	fw, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(make([]byte, n)); err != nil {
		t.Fatal(err)
	}
	if err := fw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// lyingBundle is a small valid-looking ZIP whose manifest declares
// manifestBytes, whose pitha.db header declares headerBytes and whose deflate
// stream really expands to streamBytes.
func lyingBundle(t *testing.T, manifestBytes int64, headerBytes uint64, streamBytes int) []byte {
	t.Helper()
	latest, err := migratebundle.LatestSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(migratebundle.Manifest{
		FormatVersion: migratebundle.FormatVersion, SchemaVersion: latest,
		Database: migratebundle.DatabaseName, DatabaseSHA256: "00", DatabaseBytes: manifestBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	mw, err := zw.Create(migratebundle.ManifestName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mw.Write(raw); err != nil {
		t.Fatal(err)
	}
	stream := deflated(t, streamBytes)
	dw, err := zw.CreateRaw(&zip.FileHeader{
		Name: migratebundle.DatabaseName, Method: zip.Deflate,
		CompressedSize64: uint64(len(stream)), UncompressedSize64: headerBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dw.Write(stream); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// A bundle whose declared size is huge, whose header disagrees with the
// manifest, or whose stream outgrows the declared size is refused, and no
// staging file is left behind.
func TestUnpack_RefusesSizeLies(t *testing.T) {
	const small = 1024
	tests := map[string]struct {
		bundle []byte
		want   error
	}{
		"manifest and header declare more than the cap": {lyingBundle(t, migratebundle.MaxDatabaseBytes+1, uint64(migratebundle.MaxDatabaseBytes)+1, small), migratebundle.ErrNotBundle},
		"header disagrees with the manifest":            {lyingBundle(t, small, small/2, small), migratebundle.ErrChecksum},
		"stream outgrows the declared size":             {lyingBundle(t, small, small, 8<<20), migratebundle.ErrChecksum},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dst := filepath.Join(t.TempDir(), "out.db")
			_, err := migratebundle.Unpack(context.Background(), bytes.NewReader(tc.bundle), int64(len(tc.bundle)), dst)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if _, statErr := os.Stat(dst); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("a refused bundle left %q behind (stat err %v)", dst, statErr)
			}
		})
	}
}

// Only manifest.json and pitha.db may be in the archive; anything more is
// refused from the central directory alone, however it is named.
func TestReadManifest_RefusesAnyOtherEntryCount(t *testing.T) {
	for _, extra := range []int{1, 1000} {
		t.Run(strconv.Itoa(extra), func(t *testing.T) {
			var out bytes.Buffer
			zw := zip.NewWriter(&out)
			for _, name := range []string{migratebundle.ManifestName, migratebundle.DatabaseName} {
				if _, err := zw.Create(name); err != nil {
					t.Fatal(err)
				}
			}
			for i := range extra {
				if _, err := zw.Create("extra-" + strconv.Itoa(i)); err != nil {
					t.Fatal(err)
				}
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			if _, _, err := migratebundle.ReadManifest(bytes.NewReader(out.Bytes()), int64(out.Len())); !errors.Is(err, migratebundle.ErrNotBundle) {
				t.Fatalf("err = %v, want ErrNotBundle", err)
			}
		})
	}
}
