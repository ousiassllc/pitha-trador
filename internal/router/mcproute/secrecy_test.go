package mcproute_test

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/logging"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/backup"
	"github.com/ousiassllc/pitha-trador/internal/service/dataexport"
)

// logSource serves the captured slog output as the measurement log, the way
// the real logging.Exporter serves the daily log files.
type logSource struct{ data []byte }

func (s logSource) ExportMetrics(context.Context, time.Time, time.Time) (logging.ExportResult, error) {
	return logging.ExportResult{Data: s.data}, nil
}

// captureSlog routes the default logger (also used by the router's request
// log) into a DEBUG-level JSON buffer for the test.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// The API key is shown once, in the response of the key action. It must be
// in no other response, log line, JSON/format dump of the View, data export,
// database backup or the database itself (issue #792).
func TestAPIKeyNeverLeavesTheOneRevealResponse(t *testing.T) {
	logs := captureSlog(t)
	ctx := context.Background()
	a := newApp(t, fullSecrets{})
	a.session()

	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// First generation: the key is in this response only.
	reveal := a.post("/settings/mcp/key", url.Values{}, true)
	key1 := a.loaded().APIKey
	if key1 == "" || !strings.Contains(reveal.Body.String(), key1) {
		t.Fatalf("setup: key %q not revealed by the key response", key1)
	}

	// Every later render is masked: page, panel, save success and failure.
	var renders []string
	renders = append(renders, a.get("/settings/integrations").Body.String(), a.get("/settings/mcp-panel").Body.String())
	renders = append(renders, a.save("127.0.0.1:48111", true).Body.String(), a.save("0.0.0.0:1", true).Body.String())
	for i, body := range renders {
		if strings.Contains(body, key1) {
			t.Errorf("render #%d contains the API key", i)
		}
	}

	// Regenerating reveals the new key and still no earlier one.
	reveal2 := a.post("/settings/mcp/key", url.Values{}, true)
	key2 := a.loaded().APIKey
	if key2 == key1 || !strings.Contains(reveal2.Body.String(), key2) || strings.Contains(reveal2.Body.String(), key1) {
		t.Fatalf("second reveal: key2=%q (key1=%q)", key2, key1)
	}
	keys := []string{key1, key2}

	// A request on the page that follows (nothing is revealed again).
	if body := a.get("/settings/mcp-panel").Body.String(); strings.Contains(body, key2) {
		t.Error("panel after the reveal contains the key")
	}

	// (v) The View never carries the key, whatever the formatting.
	view := a.svc.View(ctx)
	asJSON, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, dump := range []string{string(asJSON), fmt.Sprintf("%+v", view), fmt.Sprintf("%#v", view), fmt.Sprint(view)} {
		assertNoKey(t, "View dump", []byte(dump), keys)
	}

	// (ii) Log output of the service, the handlers and the request log.
	if logs.Len() == 0 {
		t.Fatal("no log output captured: the test would prove nothing")
	}
	assertNoKey(t, "slog output", logs.Bytes(), keys)

	// (iii) Data export, including the captured log as the measurement log.
	exporter := dataexport.NewLocal(db, logSource{data: logs.Bytes()})
	var archive bytes.Buffer
	if _, err := exporter.Export(ctx, &archive, dataexport.Request{
		Kinds:  []dataexport.Kind{dataexport.KindTrades, dataexport.KindDecisions, dataexport.KindMetrics},
		Period: dataexport.Period{From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour)},
		Format: dataexport.FormatCSV,
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) == 0 {
		t.Fatal("empty export archive")
	}
	assertNoKey(t, "export ZIP bytes", archive.Bytes(), keys)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		assertNoKey(t, "export entry "+f.Name, body, keys)
	}

	// (iv) Database backup (daily copy and weekly gzip) and the database.
	dest := t.TempDir()
	if err := backup.New(db, dest, 0).Backup(ctx); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	checked := 0
	err = filepath.WalkDir(dest, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".gz") {
			zr, err := gzip.NewReader(bytes.NewReader(body))
			if err != nil {
				return err
			}
			if body, err = io.ReadAll(zr); err != nil {
				return err
			}
		}
		assertNoKey(t, "backup file "+filepath.Base(path), body, keys)
		checked++
		return nil
	})
	if err != nil || checked == 0 {
		t.Fatalf("walking backup: checked=%d err=%v", checked, err)
	}
	var dbPath string
	if err := db.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&dbPath); err != nil {
		t.Fatal(err)
	}
	dbFiles, _ := filepath.Glob(dbPath + "*")
	if len(dbFiles) == 0 {
		t.Fatalf("no database files at %q", dbPath)
	}
	for _, f := range dbFiles {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		assertNoKey(t, "database file "+filepath.Base(f), body, keys)
	}
}

// The key does live in the settings file (the one place that holds it): the
// leak scans above are meaningful only because that file really has it.
func TestAPIKeyIsStoredInTheSettingsFileOnly(t *testing.T) {
	a := newApp(t, fullSecrets{})
	a.session()
	if rec := a.post("/settings/mcp/key", url.Values{}, true); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	raw, err := os.ReadFile(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(a.loaded().APIKey)) {
		t.Fatal("the settings file does not hold the key")
	}
}

func assertNoKey(t *testing.T, what string, data []byte, keys []string) {
	t.Helper()
	for _, k := range keys {
		if k != "" && bytes.Contains(data, []byte(k)) {
			t.Errorf("%s contains an API key", what)
		}
	}
}
