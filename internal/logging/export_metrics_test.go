package logging

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func exportMetrics(t *testing.T, e *Exporter, from, to time.Time) ExportResult {
	t.Helper()
	res, err := e.ExportMetrics(context.Background(), from, to)
	if err != nil {
		t.Fatalf("ExportMetrics: %v", err)
	}
	return res
}

// Issue #772: the metrics export carries the INFO records (#652 measurement
// lines) next to WARN/ERROR, inside the requested period only, in time order,
// across a plain file and a gzip archive. DEBUG never appears.
func TestExporter_MetricsIncludeInfoWithinPeriod(t *testing.T) {
	dir := t.TempDir()
	writeGzipLog(t, dir, "2026-09-29.log.gz",
		rec("2026-09-29T23:59:59Z", "INFO", "before-period")+
			"") // out of the period: only its day file is looked up when it overlaps
	writeGzipLog(t, dir, "2026-09-30.log.gz",
		rec("2026-09-30T08:00:00Z", "INFO", "rankingmeasure: ranking measured")+
			rec("2026-09-30T08:00:01Z", "DEBUG", "debug-noise"))
	writeLogFile(t, dir, "2026-10-01.log",
		rec("2026-10-01T00:00:01Z", "WARN", "warn")+
			rec("2026-10-01T09:00:00Z", "INFO", "rankingwatch: watch list updated")+
			rec("2026-10-01T10:00:00Z", "INFO", "after-period"))

	from := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	got := msgs(t, exportMetrics(t, newTestExporter(dir), from, to))
	want := []string{"rankingmeasure: ranking measured", "warn", "rankingwatch: watch list updated"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("metrics = %v, want %v", got, want)
	}
}

// The period is the record time, not the file name: a record whose time
// falls outside [from, to) is dropped even when it sits in an included file.
func TestExporter_RangeUsesRecordTime(t *testing.T) {
	dir := t.TempDir()
	writeLogFile(t, dir, "2026-10-01.log",
		rec("2026-10-01T05:00:00Z", "INFO", "early")+
			rec("2026-10-01T12:00:00Z", "INFO", "at-from")+
			rec("2026-10-01T18:00:00Z", "INFO", "at-to"))
	from := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)
	got := msgs(t, exportMetrics(t, newTestExporter(dir), from, to))
	if strings.Join(got, "|") != "at-from" {
		t.Fatalf("records = %v, want only [at-from] (to is exclusive)", got)
	}
}

func TestExporter_RangeRejectsBadPeriods(t *testing.T) {
	e := newTestExporter(t.TempDir())
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, err := e.ExportRange(context.Background(), at, at, slog.LevelInfo); err == nil {
		t.Error("an empty period was accepted")
	}
	if _, err := e.ExportRange(context.Background(), at, at.Add(-time.Hour), slog.LevelInfo); err == nil {
		t.Error("an inverted period was accepted")
	}
	if _, err := e.ExportRange(context.Background(), at.AddDate(-20, 0, 0), at, slog.LevelInfo); err == nil {
		t.Error("a period of more than maxExportRangeDays days was accepted")
	}
}

// An absent day file contributes nothing and is not an error, as in Export.
func TestExporter_RangeMissingDaysAreEmpty(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	res := exportMetrics(t, newTestExporter(t.TempDir()), from, from.AddDate(0, 0, 3))
	if res.Records != 0 || len(res.Data) != 0 || res.Truncated {
		t.Fatalf("result = %+v, want empty", res)
	}
}

// The same MaxExportBytes cap as Export: newest records win, Truncated set.
func TestExporter_RangeTruncatesOldestFirst(t *testing.T) {
	dir := t.TempDir()
	line1 := rec("2026-10-01T01:00:00Z", "INFO", "oldest")
	line2 := rec("2026-10-01T02:00:00Z", "INFO", "middle")
	line3 := rec("2026-10-01T03:00:00Z", "INFO", "newest")
	writeLogFile(t, dir, "2026-10-01.log", line1+line2+line3)
	e := newTestExporter(dir)
	e.maxBytes = len(line2) + len(line3)

	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	res := exportMetrics(t, e, from, from.AddDate(0, 0, 1))
	if got := strings.Join(msgs(t, res), "|"); got != "middle|newest" || !res.Truncated {
		t.Fatalf("records = %s truncated=%v, want middle|newest and truncated", got, res.Truncated)
	}
}

// Issue #772 redaction: the INFO metrics export masks tokens, passwords, API
// keys and the second password (第二暗証番号) in attributes and in strings,
// exactly like the error-log export.
func TestExporter_MetricsMaskSecrets(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		`{"time":"2026-10-01T00:00:01Z","level":"INFO","msg":"marketdata: token issued","token":"TOKEN-SECRET","api_key":"APIKEY-SECRET","password":"PASSWORD-SECRET","second_password":"SECOND-SECRET","second":"SECOND-SECRET-2","Authorization":"Bearer AUTH-SECRET"}`,
		`{"time":"2026-10-01T00:00:02Z","level":"INFO","msg":"rankingmeasure: ranking measured","duration_ms":12,"result_count":50}`,
		`{"time":"2026-10-01T00:00:03Z","level":"INFO","msg":"tachibana: login ok","sAuthId":"AUTHID-SECRET","sUrlRequest":"URL-SECRET","private_key":"PEM-SECRET"}`,
		`{"time":"2026-10-01T00:00:04Z","level":"INFO","msg":"request done","error":"Post \"https://kabuka.e-shiten.jp/e_api_v4r10/request/MjAyNjEwMDgwMDAwMDBUT0tFTjEyMzQ1Ng/?sAuthId=AUTHID-SECRET\": EOF"}`,
	}
	writeLogFile(t, dir, "2026-10-01.log", strings.Join(lines, "\n")+"\n")

	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	res := exportMetrics(t, newTestExporter(dir), from, from.AddDate(0, 0, 1))
	if res.Records != 4 {
		t.Fatalf("Records = %d, want 4: %s", res.Records, res.Data)
	}
	for _, secret := range []string{"TOKEN-SECRET", "APIKEY-SECRET", "PASSWORD-SECRET", "SECOND-SECRET", "AUTH-SECRET", "AUTHID-SECRET", "URL-SECRET", "PEM-SECRET", "MjAyNjEwMDgw"} {
		if strings.Contains(string(res.Data), secret) {
			t.Errorf("secret %q survived the metrics export:\n%s", secret, res.Data)
		}
	}
	// The measurement values themselves are kept.
	if !strings.Contains(string(res.Data), `"duration_ms":12`) || !strings.Contains(string(res.Data), `"result_count":50`) {
		t.Errorf("measurement attributes were lost:\n%s", res.Data)
	}
}
