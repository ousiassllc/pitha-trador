package startup_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/startup"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/dataexport"
)

// isolateLogging restores the process-wide slog default after the test and
// points the DB (hence the default log directory) into a temp dir.
func isolateLogging(t *testing.T) (dbPath string) {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	dbPath = filepath.Join(t.TempDir(), "data", "pitha.db")
	t.Setenv(bootstrap.EnvDBPath, dbPath)
	return dbPath
}

// resolveLogDir is the directory half of bootstrap.ResolveLogSetup.
func resolveLogDir() (string, error) {
	setup, err := bootstrap.ResolveLogSetup()
	return setup.Dir, err
}

func TestResolveLogDir_NextToDBAndIndependentOfWorkingDirectory(t *testing.T) {
	dbPath := isolateLogging(t)
	t.Chdir(t.TempDir())

	got, err := resolveLogDir()
	if err != nil {
		t.Fatalf("ResolveLogDir: %v", err)
	}
	if want := filepath.Join(filepath.Dir(dbPath), "logs"); got != want {
		t.Fatalf("ResolveLogDir = %q, want %q", got, want)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("ResolveLogDir = %q, want an absolute path", got)
	}
}

// storeLogDirSetting writes the Settings screen's log directory (issue #708)
// into the database at dbPath, the way the Settings screen stores it.
func storeLogDirSetting(t *testing.T, dbPath, jsonValue string) {
	t.Helper()
	storeSetting(t, dbPath, config.KeyLogDir, jsonValue)
}

func storeSetting(t *testing.T, dbPath, key, jsonValue string) {
	t.Helper()
	conn, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO runtime_settings (key, value, updated_at) VALUES (?, ?, '2026-10-08T00:00:00Z')`, key, jsonValue); err != nil {
		t.Fatalf("store log dir setting: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
}

func TestResolveLogDir_UsesSettingsLogDir(t *testing.T) {
	dbPath := isolateLogging(t)
	override := filepath.Join(t.TempDir(), "custom-logs")
	encoded, err := json.Marshal(override)
	if err != nil {
		t.Fatal(err)
	}
	storeLogDirSetting(t, dbPath, string(encoded))

	got, err := resolveLogDir()
	if err != nil || got != override {
		t.Fatalf("ResolveLogDir = (%q, %v), want %q", got, err, override)
	}
}

func TestResolveLogDir_InvalidSettingFallsBackToDefault(t *testing.T) {
	dbPath := isolateLogging(t)
	storeLogDirSetting(t, dbPath, `"relative/logs"`)

	got, err := resolveLogDir()
	if want := filepath.Join(filepath.Dir(dbPath), "logs"); err != nil || got != want {
		t.Fatalf("ResolveLogDir = (%q, %v), want the default %q", got, err, want)
	}
}

func TestResolveLogSetup_LevelFollowsSettings(t *testing.T) {
	dbPath := isolateLogging(t)

	setup, err := bootstrap.ResolveLogSetup()
	if err != nil || setup.Level != slog.LevelInfo {
		t.Fatalf("unset: ResolveLogSetup = (%+v, %v), want INFO", setup, err)
	}

	storeSetting(t, dbPath, config.KeyLogLevel, `"warn"`)
	setup, err = bootstrap.ResolveLogSetup()
	if err != nil || setup.Level != slog.LevelWarn {
		t.Fatalf("stored warn: ResolveLogSetup = (%+v, %v), want WARN", setup, err)
	}
}

func TestResolveLogSetup_InvalidLevelFallsBackToInfo(t *testing.T) {
	dbPath := isolateLogging(t)
	storeSetting(t, dbPath, config.KeyLogLevel, `"loud"`)

	setup, err := bootstrap.ResolveLogSetup()
	if err != nil || setup.Level != slog.LevelInfo {
		t.Fatalf("ResolveLogSetup = (%+v, %v), want INFO", setup, err)
	}
}

// The writer (ResolveLogDir), the Archiver and the Exporter
// (State.Paths.LogDir, built from it by BuildServices) must name one
// directory: records written through the logger are exported.
func TestLogDir_WriterArchiverAndExporterShareOneDirectory(t *testing.T) {
	dbPath := isolateLogging(t)
	t.Chdir(t.TempDir())

	state, err := bootstrap.Run(bootstrap.Config{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer func() { _ = state.Close() }()
	dir, err := resolveLogDir()
	if err != nil {
		t.Fatalf("ResolveLogDir: %v", err)
	}
	if state.Paths.LogDir != dir || state.Paths.DBPath != dbPath {
		t.Fatalf("State.Paths = %+v, want LogDir %q", state.Paths, dir)
	}

	startup.RunMain("test", bootstrap.ResolveLogSetup, func() error { slog.Error("shared dir marker"); return nil })

	services := bootstrap.BuildServices(state, config.Secrets{})
	res, err := services.ErrorLogs.Export(context.Background(), 1, slog.LevelError)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !bytes.Contains(res.Data, []byte("shared dir marker")) {
		t.Fatalf("exported records do not contain the logged error: %q", res.Data)
	}
}

// issue #773: dataexport.NewLocal (as RouterOptions wires it) reads the same log directory as the writer
// (measurement log) and the real database (trade / decision log) - one ZIP
// with all three kinds and a manifest.
func TestDataExport_ExportsAllKindsFromTheSharedLogDirAndDB(t *testing.T) {
	isolateLogging(t)
	t.Chdir(t.TempDir())

	state, err := bootstrap.Run(bootstrap.Config{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer func() { _ = state.Close() }()
	startup.RunMain("test", bootstrap.ResolveLogSetup, func() error { slog.Error("data export marker"); return nil })

	services := bootstrap.BuildServices(state, config.Secrets{})
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var buf bytes.Buffer
	manifest, err := dataexport.NewLocal(state.DB, services.ErrorLogs).Export(context.Background(), &buf, dataexport.Request{
		Kinds:  []dataexport.Kind{dataexport.KindTrades, dataexport.KindDecisions, dataexport.KindMetrics},
		Period: dataexport.Period{From: today.AddDate(0, 0, -1), To: today.AddDate(0, 0, 2)},
		Format: dataexport.FormatJSONL,
	})
	if err != nil || len(manifest.Kinds) != 3 {
		t.Fatalf("Export: kinds = %+v, err = %v, want trades, decisions, metrics", manifest.Kinds, err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != "metrics/metrics.jsonl" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		body, _ := io.ReadAll(rc)
		_ = rc.Close()
		if !bytes.Contains(body, []byte("data export marker")) {
			t.Fatalf("metrics file lacks the logged record: %q", body)
		}
		return
	}
	t.Fatal("archive has no metrics/metrics.jsonl")
}

func readLogRecords(t *testing.T, dir string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, time.Now().UTC().Format("2006-01-02")+".log"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	var records []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		records = append(records, rec)
	}
	return records
}

// A fatal startup error (bootstrap.Run / LoadSecrets / lock / wails.Run)
// must reach the log file at ERROR level, exactly once.
func TestRunMain_LogsReturnedErrorAtErrorLevelOnce(t *testing.T) {
	isolateLogging(t)
	dir, err := resolveLogDir()
	if err != nil {
		t.Fatal(err)
	}

	if code := startup.RunMain("test", bootstrap.ResolveLogSetup, func() error { return errors.New("db is gone") }); code != 1 {
		t.Fatalf("RunMain exit code = %d, want 1", code)
	}
	var errorRecords []map[string]any
	for _, rec := range readLogRecords(t, dir) {
		if rec["level"] == "ERROR" {
			errorRecords = append(errorRecords, rec)
		}
	}
	if len(errorRecords) != 1 || errorRecords[0]["error"] != "db is gone" {
		t.Fatalf("ERROR records = %v, want exactly one carrying error=%q", errorRecords, "db is gone")
	}
}

func TestRunMain_SuccessExitsZeroWithoutErrorRecords(t *testing.T) {
	isolateLogging(t)
	dir, err := resolveLogDir()
	if err != nil {
		t.Fatal(err)
	}
	if code := startup.RunMain("test", bootstrap.ResolveLogSetup, func() error { slog.Info("hello"); return nil }); code != 0 {
		t.Fatalf("RunMain exit code = %d, want 0", code)
	}
	for _, rec := range readLogRecords(t, dir) {
		if rec["level"] == "ERROR" {
			t.Fatalf("unexpected ERROR record %v", rec)
		}
	}
}

// An unwritable log directory must not stop the app from starting: logging
// falls back to stderr and the run function still executes.
func TestRunMain_UnwritableLogDirFallsBackToStderr(t *testing.T) {
	dbPath := isolateLogging(t)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(filepath.Join(blocker, "logs")) // parent is a regular file
	if err != nil {
		t.Fatal(err)
	}
	storeLogDirSetting(t, dbPath, string(encoded))

	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stderr.Close() }()
	prevStderr := os.Stderr
	os.Stderr = stderr
	t.Cleanup(func() { os.Stderr = prevStderr })

	ran := false
	code := startup.RunMain("test", bootstrap.ResolveLogSetup, func() error { ran = true; return errors.New("late failure") })
	if !ran || code != 1 {
		t.Fatalf("ran = %v, exit code = %d; want the run function to execute and exit 1", ran, code)
	}
	out, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"file logging unavailable", "late failure"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("stderr = %q, want it to contain %q", out, want)
		}
	}
}
