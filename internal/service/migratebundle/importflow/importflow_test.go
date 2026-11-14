package importflow_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/backup"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/bundletest"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/importflow"
)

const newPCSecret = "dummy-not-a-secret-newpc"

func exportBundle(t *testing.T, conn *sql.DB) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := migratebundle.NewExporter(conn).Export(context.Background(), &buf); err != nil {
		t.Fatalf("Export: %v", err)
	}
	return buf.Bytes()
}

func fixedNow() time.Time { return time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC) }

// restart simulates quitting the application: the live connection is closed,
// ApplyPending runs, and the database is opened again the normal way.
func restart(t *testing.T, conn *sql.DB, path string) (*importflow.Result, *sql.DB) {
	t.Helper()
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	res := importflow.ApplyPending(context.Background(), path, fixedNow)
	reopened, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("reopen after import: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	return res, reopened
}

// newPC is the importing environment: a database with the PC's own secret and
// directory settings (what the Setup screen and Settings left behind) and,
// when withData, a row of user data.
func newPC(t *testing.T, withData bool) (*sql.DB, string) {
	conn, path := bundletest.OpenDB(t)
	bundletest.Exec(t, conn,
		`INSERT INTO secrets (key, encrypted_value, updated_at) VALUES ('JEV_API_KEY', '`+newPCSecret+`', 't')`,
		`INSERT INTO runtime_settings (key, value, updated_at) VALUES ('`+config.KeyBackupDir+`', '"/mnt/new-pc/backups"', 't'), ('screener.top_n', '99', 't')`,
	)
	if withData {
		bundletest.Exec(t, conn, `INSERT INTO instruments (symbol, name, market, is_active, created_at, updated_at) VALUES ('1111', 'n', 'TSE', 1, 't', 't')`)
	}
	return conn, path
}

func singleValue(t *testing.T, conn *sql.DB, q string) string {
	t.Helper()
	var v string
	if err := conn.QueryRow(q).Scan(&v); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return v
}

// Export on the old PC -> import on the new one: per-table row counts of every
// table (from sqlite_master) match, the new PC keeps its own secret and
// directory setting, and nothing of the old PC's secrets arrives.
func TestImport_RoundTripReconcilesAndKeepsThisPCsSecrets(t *testing.T) {
	src, _ := bundletest.OpenDB(t)
	bundletest.Seed(t, src)
	bundle := exportBundle(t, src)
	srcCounts := bundletest.Counts(t, src)

	target, path := newPC(t, true)
	svc := importflow.New(target, path)
	ctx := context.Background()

	preview, err := svc.Preview(ctx, bytes.NewReader(bundle))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if !preview.TargetHasData || preview.Manifest.AppVersion == "" || preview.Manifest.CreatedAt == "" {
		t.Fatalf("preview = %+v", preview)
	}
	rows := map[string]importflow.TableComparison{}
	for _, tc := range preview.Tables {
		rows[tc.Name] = tc
	}
	if rows["instruments"].Current != 1 || rows["instruments"].Bundle != 2 {
		t.Errorf("preview instruments = %+v, want current 1 / bundle 2", rows["instruments"])
	}
	// Nothing changed yet.
	if got := bundletest.Counts(t, target)["instruments"]; got != 1 {
		t.Fatalf("preview changed the live database: instruments = %d", got)
	}

	if _, err := svc.Confirm(ctx, preview.Token, false); !errors.Is(err, importflow.ErrReplaceNotConfirmed) {
		t.Fatalf("Confirm without replace: err = %v, want ErrReplaceNotConfirmed", err)
	}
	if p, _ := svc.Pending(); p != nil {
		t.Fatal("a refused Confirm left a pending import")
	}
	pend, err := svc.Confirm(ctx, preview.Token, true)
	if err != nil || !pend.ReplacedExisting {
		t.Fatalf("Confirm: %+v, %v", pend, err)
	}
	if got := bundletest.Counts(t, target)["instruments"]; got != 1 {
		t.Fatalf("Confirm changed the live database: instruments = %d", got)
	}

	res, imported := restart(t, target, path)
	if res == nil || res.Status != importflow.StatusApplied || res.SafetyCopy == "" {
		t.Fatalf("result = %+v", res)
	}
	if err := backup.VerifyCopy(ctx, res.SafetyCopy); err != nil {
		t.Fatalf("safety copy: %v", err)
	}
	safety, err := backup.OpenCopy(res.SafetyCopy)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = safety.Close() }()
	if got := bundletest.Counts(t, safety)["instruments"]; got != 1 {
		t.Errorf("safety copy instruments = %d, want the replaced database's 1", got)
	}

	got := bundletest.Counts(t, imported)
	names := make([]string, 0, len(srcCounts))
	for name := range srcCounts {
		names = append(names, name)
	}
	sort.Strings(names)
	var report strings.Builder
	report.WriteString("table | old PC | new PC after import\n")
	for _, name := range names {
		want := srcCounts[name]
		switch name {
		case "secrets":
			want = 1 // the new PC's own
		case "runtime_settings":
			want = 3 // 2 non-secret from the bundle + the new PC's backup_dir
		}
		report.WriteString(name + " | " + strconv.FormatInt(srcCounts[name], 10) + " | " + strconv.FormatInt(got[name], 10) + "\n")
		if got[name] != want {
			t.Errorf("table %s: %d rows after import, want %d", name, got[name], want)
		}
	}
	t.Log("\n" + report.String())

	if v := singleValue(t, imported, `SELECT encrypted_value FROM secrets WHERE key='JEV_API_KEY'`); v != newPCSecret {
		t.Errorf("secret after import = %q, want this PC's own", v)
	}
	if v := singleValue(t, imported, `SELECT value FROM runtime_settings WHERE key='`+config.KeyBackupDir+`'`); v != `"/mnt/new-pc/backups"` {
		t.Errorf("backup_dir after import = %q, want this PC's own", v)
	}
	if v := singleValue(t, imported, `SELECT value FROM runtime_settings WHERE key='screener.top_n'`); v != "7" {
		t.Errorf("screener.top_n = %q, want the bundle's 7", v)
	}
	if raw, err := os.ReadFile(path); err != nil || bytes.Contains(raw, []byte(bundletest.Dummy)) {
		t.Errorf("the old PC's dummy secret reached the new database (read err %v)", err)
	}
	if p, _ := importflow.New(imported, path).Pending(); p != nil {
		t.Error("pending import not cleared after apply")
	}
	if last, _ := importflow.New(imported, path).LastResult(); last == nil || last.Status != importflow.StatusApplied {
		t.Errorf("LastResult = %+v", last)
	}
}

// A fresh target (no user data) needs no replace confirmation.
func TestConfirm_FreshTargetNeedsNoReplaceConfirmation(t *testing.T) {
	src, _ := bundletest.OpenDB(t)
	bundletest.Seed(t, src)
	bundle := exportBundle(t, src)
	target, path := bundletest.OpenDB(t)
	svc := importflow.New(target, path)
	preview, err := svc.Preview(context.Background(), bytes.NewReader(bundle))
	if err != nil || preview.TargetHasData {
		t.Fatalf("Preview: %+v, %v", preview, err)
	}
	if _, err := svc.Confirm(context.Background(), preview.Token, false); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	res, imported := restart(t, target, path)
	if res == nil || res.Status != importflow.StatusApplied || res.SafetyCopy == "" {
		t.Fatalf("result = %+v", res)
	}
	if got := bundletest.Counts(t, imported)["market_snapshots"]; got != 3 {
		t.Errorf("market_snapshots = %d, want 3", got)
	}
}

// A bundle of an older release is migrated to this build's schema by the
// import, before the live database is replaced.
func TestImport_OlderSchemaBundleIsMigrated(t *testing.T) {
	latest, err := migratebundle.LatestSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	old, _ := bundletest.OpenDBAtVersion(t, latest-1)
	bundletest.Seed(t, old)
	bundle := exportBundle(t, old)
	target, path := bundletest.OpenDB(t)
	svc := importflow.New(target, path)
	preview, err := svc.Preview(context.Background(), bytes.NewReader(bundle))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if preview.Manifest.SchemaVersion != latest-1 || preview.CurrentSchemaVersion != latest {
		t.Fatalf("preview schema = %d / %d", preview.Manifest.SchemaVersion, preview.CurrentSchemaVersion)
	}
	if _, err := svc.Confirm(context.Background(), preview.Token, false); err != nil {
		t.Fatal(err)
	}
	res, imported := restart(t, target, path)
	if res == nil || res.Status != importflow.StatusApplied {
		t.Fatalf("result = %+v", res)
	}
	if v, _, err := migratebundle.SchemaVersion(context.Background(), imported); err != nil || v != latest {
		t.Fatalf("schema after import = %d (%v), want %d", v, err, latest)
	}
	if got := bundletest.Counts(t, imported)["instruments"]; got != 2 {
		t.Errorf("instruments = %d, want 2", got)
	}
}
