package migratebundle_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/backup"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/bundletest"
)

func export(t *testing.T, ex *migratebundle.Exporter) ([]byte, migratebundle.Manifest) {
	t.Helper()
	var buf bytes.Buffer
	m, err := ex.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	return buf.Bytes(), m
}

func unpack(t *testing.T, bundle []byte) (string, migratebundle.Manifest, error) {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "out.db")
	m, err := migratebundle.Unpack(context.Background(), bytes.NewReader(bundle), int64(len(bundle)), dst)
	return dst, m, err
}

// The bundle round-trips: every table of the source database, enumerated
// from sqlite_master, has the same row count after unpacking, except that the
// excluded settings are gone and the secrets table is empty.
func TestExportUnpack_ReconcilesPerTableRowCounts(t *testing.T) {
	conn, _ := bundletest.OpenDB(t)
	bundletest.Seed(t, conn)
	bundle, m := export(t, migratebundle.NewExporter(conn))

	dst, got, err := unpack(t, bundle)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if got.SchemaVersion != m.SchemaVersion || got.SecretsIncluded {
		t.Fatalf("manifest = %+v", got)
	}
	out, err := backup.OpenCopy(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Close() }()

	src, restored := bundletest.Counts(t, conn), bundletest.Counts(t, out)
	if len(src) < 20 {
		t.Fatalf("only %d tables enumerated from sqlite_master", len(src))
	}
	var report strings.Builder
	report.WriteString("table | source | bundle\n")
	for _, tc := range m.Tables {
		report.WriteString(tc.Name + " | " + strconv.FormatInt(src[tc.Name], 10) + " | " + strconv.FormatInt(restored[tc.Name], 10) + "\n")
	}
	t.Log("\n" + report.String())
	for name, n := range src {
		want := n
		switch name {
		case "secrets":
			want = 0
		case "runtime_settings":
			want = n - 3 // backup_dir, log_dir, demo private key path
		}
		if restored[name] != want {
			t.Errorf("table %s: bundle has %d rows, want %d (source %d)", name, restored[name], want, n)
		}
	}
	for table, want := range map[string]int64{"instruments": 2, "trade_signals": 2, "paper_orders": 1, "market_snapshots": 3, "jev_decisions": 1, "calibration_outcomes": 2, "kill_switch_events": 1} {
		if restored[table] != want {
			t.Errorf("table %s: %d rows, want %d", table, restored[table], want)
		}
	}
}

// The default bundle carries no secret: not in the secrets table, not in the
// machine-specific settings, and not in the database's free pages either.
func TestExport_IsSecretFree(t *testing.T) {
	conn, _ := bundletest.OpenDB(t)
	bundletest.Seed(t, conn)
	bundle, m := export(t, migratebundle.NewExporter(conn))

	dst, _, err := unpack(t, bundle)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	raw, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(bundletest.Dummy)) {
		t.Fatalf("the bundle's database contains %q", bundletest.Dummy)
	}
	if m.SecretsIncluded {
		t.Error("manifest claims secrets are included")
	}
	for _, key := range []string{config.KeyBackupDir, config.KeyLogDir, config.KeyTachibanaDemoPrivateKeyPath} {
		if !slices.Contains(m.ExcludedSettings, key) {
			t.Errorf("manifest.excluded_settings lacks %q: %v", key, m.ExcludedSettings)
		}
	}
	if bytes.Contains(bundle, []byte(bundletest.Dummy)) {
		t.Error("the archive bytes contain the dummy secret")
	}
}

// A setting whose value the masking rules would rewrite (a webhook URL pasted
// into a setting) is dropped from the bundle and named in the manifest.
func TestExport_DropsSecretBearingSettingAndSaysSo(t *testing.T) {
	conn, _ := bundletest.OpenDB(t)
	bundletest.Seed(t, conn)
	bundletest.Exec(t, conn, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('screener.note', '"https://hooks.slack.com/services/T000/B000/`+bundletest.Dummy+`"', 't')`)
	bundle, m := export(t, migratebundle.NewExporter(conn))
	if !slices.Contains(m.ExcludedSettings, "screener.note") {
		t.Fatalf("excluded_settings = %v", m.ExcludedSettings)
	}
	dst, _, err := unpack(t, bundle)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if raw, _ := os.ReadFile(dst); bytes.Contains(raw, []byte(bundletest.Dummy)) {
		t.Fatal("secret-bearing setting is still in the file")
	}
}

// The export reads through one VACUUM INTO snapshot: rows written by the live
// application before it started are all in, the live database is unchanged
// and keeps accepting writes, and the copy passes integrity_check.
func TestExport_SnapshotLeavesLiveDatabaseUsable(t *testing.T) {
	conn, _ := bundletest.OpenDB(t)
	bundletest.Seed(t, conn)
	before := bundletest.Counts(t, conn)
	bundle, _ := export(t, migratebundle.NewExporter(conn))
	bundletest.Exec(t, conn, `INSERT INTO instruments (symbol, name, market, is_active, created_at, updated_at) VALUES ('9984', 'z', 'TSE', 1, 't', 't')`)
	after := bundletest.Counts(t, conn)
	if after["instruments"] != before["instruments"]+1 || after["secrets"] != before["secrets"] {
		t.Fatalf("live database changed by the export: before %v after %v", before, after)
	}
	dst, m, err := unpack(t, bundle)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if err := backup.VerifyCopy(context.Background(), dst); err != nil {
		t.Fatal(err)
	}
	for _, tc := range m.Tables {
		if tc.Name == "instruments" && tc.Rows != before["instruments"] {
			t.Errorf("snapshot instruments = %d, want %d", tc.Rows, before["instruments"])
		}
	}
}

func TestReadManifest_RefusesNewerSchemaAndUnknownFormat(t *testing.T) {
	conn, _ := bundletest.OpenDB(t)
	bundle, m := export(t, migratebundle.NewExporter(conn))
	for name, tc := range map[string]struct {
		edit func(*migratebundle.Manifest)
		want error
	}{
		"newer schema": {func(m *migratebundle.Manifest) { m.SchemaVersion += 1 }, migratebundle.ErrSchemaTooNew},
		"newer format": {func(m *migratebundle.Manifest) { m.FormatVersion = 99 }, migratebundle.ErrUnsupportedFormat},
	} {
		t.Run(name, func(t *testing.T) {
			edited := rewriteManifest(t, bundle, m, tc.edit)
			if _, _, err := unpack(t, edited); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// While a writer keeps inserting, every export is still one consistent
// snapshot: the copy passes integrity_check, and each table's manifest row
// count equals the rows really in the unpacked database (Unpack checks it).
func TestExport_ConsistentWhileTheApplicationKeepsWriting(t *testing.T) {
	conn, _ := bundletest.OpenDB(t)
	bundletest.Seed(t, conn)

	started, stop, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		var once sync.Once
		for i := 0; ; i++ {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			if _, err := conn.Exec(`INSERT INTO market_snapshots (instrument_id, symbol, timestamp, price, volume, turnover, vwap, price_vs_vwap_bps, raw_data_json) VALUES (1, '7203', ?, 1000, 1, 1000, 1000, 0, '{}')`, "2026-11-02T00:00:00Z#"+strconv.Itoa(i)); err != nil {
				done <- err
				return
			}
			once.Do(func() { close(started) })
		}
	}()
	<-started
	for range 3 {
		bundle, m := export(t, migratebundle.NewExporter(conn))
		dst, _, err := unpack(t, bundle)
		if err != nil {
			t.Fatalf("Unpack of an export taken during writes: %v", err)
		}
		if err := backup.VerifyCopy(context.Background(), dst); err != nil {
			t.Fatal(err)
		}
		if m.SecretsIncluded {
			t.Fatal("secrets included")
		}
	}
	close(stop)
	if err := <-done; err != nil {
		t.Fatalf("writer: %v", err)
	}
}
