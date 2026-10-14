package dataexport_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/logging"
	"github.com/ousiassllc/pitha-trador/internal/service/dataexport"
)

type fakeMetricsSource struct {
	res      logging.ExportResult
	err      error
	from, to time.Time
}

func (f *fakeMetricsSource) ExportMetrics(_ context.Context, from, to time.Time) (logging.ExportResult, error) {
	f.from, f.to = from, to
	return f.res, f.err
}

func exportMetrics(t *testing.T, src dataexport.MetricsSource, format dataexport.Format) (dataexport.Manifest, map[string]string, error) {
	t.Helper()
	svc := dataexport.New(dataexport.Metrics{Source: src})
	var buf bytes.Buffer
	manifest, err := svc.Export(context.Background(), &buf, dataexport.Request{Kinds: []dataexport.Kind{dataexport.KindMetrics}, Period: period, Format: format})
	if err != nil {
		return manifest, nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		files[f.Name] = string(b)
	}
	return manifest, files, nil
}

func TestExport_MetricsRowsPerRecordInTimeOrder(t *testing.T) {
	src := &fakeMetricsSource{res: logging.ExportResult{Records: 2, Data: []byte(
		`{"time":"2026-09-28T00:10:00.5Z","level":"INFO","msg":"rankingmeasure: ranking measured","hit":3}` + "\n" +
			`{"time":"2026-09-28T00:20:00Z","level":"WARN","msg":"slow"}` + "\n")}}
	manifest, files, err := exportMetrics(t, src, dataexport.FormatCSV)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !src.from.Equal(period.From) || !src.to.Equal(period.To) {
		t.Fatalf("source asked for [%s, %s), want the request period", src.from, src.to)
	}
	if len(manifest.Kinds) != 1 || manifest.Kinds[0].Kind != dataexport.KindMetrics || manifest.Kinds[0].Rows != 2 || manifest.Kinds[0].Files[0].Truncated {
		t.Fatalf("manifest kinds = %+v, want metrics with 2 rows, not truncated", manifest.Kinds)
	}
	rows := csvRecords(t, files["metrics/metrics.csv"])
	if len(rows) != 2 || rows[0]["time"] != "2026-09-28T00:10:00.5Z" || rows[0]["level"] != "INFO" ||
		rows[0]["msg"] != "rankingmeasure: ranking measured" || !strings.Contains(rows[0]["record"], `"hit":3`) || rows[1]["level"] != "WARN" {
		t.Fatalf("metrics rows = %v", rows)
	}
}

func TestExport_MetricsEmptyPeriodHasHeaderOnly(t *testing.T) {
	manifest, files, err := exportMetrics(t, &fakeMetricsSource{}, dataexport.FormatJSONL)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if manifest.Kinds[0].Rows != 0 || files["metrics/metrics.jsonl"] != "" {
		t.Fatalf("rows = %d, file = %q, want an empty metrics file", manifest.Kinds[0].Rows, files["metrics/metrics.jsonl"])
	}
}

func TestExport_MetricsTruncationIsInManifest(t *testing.T) {
	src := &fakeMetricsSource{res: logging.ExportResult{Truncated: true, Data: []byte(`{"time":"2026-09-28T00:10:00Z","level":"INFO","msg":"m"}` + "\n")}}
	manifest, files, err := exportMetrics(t, src, dataexport.FormatCSV)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !manifest.Kinds[0].Files[0].Truncated || !strings.Contains(files[dataexport.ManifestFile], `"truncated": true`) {
		t.Fatalf("truncation not reported: %+v\n%s", manifest.Kinds[0].Files[0], files[dataexport.ManifestFile])
	}
}

func TestExport_MetricsSourceErrorFailsExport(t *testing.T) {
	boom := errors.New("boom")
	if _, _, err := exportMetrics(t, &fakeMetricsSource{err: boom}, dataexport.FormatCSV); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap the source error", err)
	}
}

// The real logging.Exporter behind the collector: records outside the period
// and below INFO are left out, and secrets in the log are masked.
func TestExport_MetricsFromLogDirMasksSecrets(t *testing.T) {
	dir := t.TempDir()
	lines := strings.Join([]string{
		`{"time":"2026-09-27T23:00:00Z","level":"INFO","msg":"before period"}`,
		`{"time":"2026-09-28T00:10:00Z","level":"INFO","msg":"ranking measured","api_key":"` + secretAPIKey + `","note":"Authorization: Bearer ` + secretBearer + `"}`,
		`{"time":"2026-09-28T00:11:00Z","level":"DEBUG","msg":"debug noise"}`,
		`{"time":"2026-09-28T03:00:00Z","level":"INFO","msg":"after period"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "2026-09-28.log"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	_, files, err := exportMetrics(t, logging.NewExporter(dir), dataexport.FormatJSONL)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	body := files["metrics/metrics.jsonl"]
	if got := len(jsonlRecords(t, body)); got != 1 {
		t.Fatalf("metrics rows = %d, want only the in-period INFO record:\n%s", got, body)
	}
	for _, leak := range []string{secretAPIKey, secretBearer, "debug noise", "before period", "after period"} {
		if strings.Contains(body, leak) {
			t.Errorf("metrics file contains %q:\n%s", leak, body)
		}
	}
	if !strings.Contains(body, logging.RedactedValue) {
		t.Errorf("no redaction marker in metrics file:\n%s", body)
	}
}
