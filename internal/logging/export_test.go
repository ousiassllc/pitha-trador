package logging

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var exportNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func newTestExporter(dir string) *Exporter {
	e := NewExporter(dir)
	e.now = func() time.Time { return exportNow }
	return e
}

func writeGzipLog(t *testing.T, dir, name, content string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(content)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	writeLogFile(t, dir, name, buf.String())
}

func rec(at, level, msg string) string {
	return fmt.Sprintf(`{"time":%q,"level":%q,"msg":%q}`+"\n", at, level, msg)
}

func export(t *testing.T, e *Exporter, days int, min slog.Level) ExportResult {
	t.Helper()
	res, err := e.Export(context.Background(), days, min)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	return res
}

func msgs(t *testing.T, res ExportResult) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(strings.TrimSuffix(string(res.Data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var r struct {
			Msg string `json:"msg"`
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("output line is not JSON: %q: %v", line, err)
		}
		out = append(out, r.Msg)
	}
	if res.Records != len(out) {
		t.Fatalf("Records = %d, but body has %d lines", res.Records, len(out))
	}
	return out
}

func TestExporter_FiltersLevelsAndDropsUnusableLines(t *testing.T) {
	dir := t.TempDir()
	huge := `{"time":"2026-10-01T01:00:00Z","level":"ERROR","msg":"` + strings.Repeat("x", maxExportLineBytes) + `"}` + "\n"
	writeLogFile(t, dir, "2026-10-01.log",
		rec("2026-10-01T00:00:01Z", "INFO", "info")+
			rec("2026-10-01T00:00:02Z", "WARN", "warn")+
			rec("2026-10-01T00:00:03Z", "ERROR", "error")+
			rec("2026-10-01T00:00:04Z", "ERROR+2", "above-error")+
			"not json at all\n"+
			`{"time":"2026-10-01T00:00:05Z","msg":"no level"}`+"\n"+
			huge+
			`{"time":"2026-10-01T00:00:06Z","level":"ERROR","msg":"unterminated tail"}`) // active file: last line has no newline yet

	e := newTestExporter(dir)

	got := msgs(t, export(t, e, 1, slog.LevelError))
	if want := []string{"error", "above-error"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("error-level messages = %v, want %v", got, want)
	}
	got = msgs(t, export(t, e, 1, slog.LevelWarn))
	if want := []string{"warn", "error", "above-error"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("warn-level messages = %v, want %v", got, want)
	}
}

func TestExporter_KeepsLineExactlyAtLimit(t *testing.T) {
	dir := t.TempDir()
	prefix := `{"level":"ERROR","msg":"`
	suffix := `"}`
	line := prefix + strings.Repeat("y", maxExportLineBytes-len(prefix)-len(suffix)) + suffix
	writeLogFile(t, dir, "2026-10-01.log", line+"\n")

	res := export(t, newTestExporter(dir), 1, slog.LevelError)
	if res.Records != 1 {
		t.Fatalf("Records = %d, want 1 for a line of exactly %d bytes", res.Records, maxExportLineBytes)
	}
}

func TestExporter_WindowCoversDaysBackAndReadsArchives(t *testing.T) {
	dir := t.TempDir()
	writeLogFile(t, dir, "2026-10-01.log", rec("2026-10-01T00:00:00Z", "ERROR", "today"))
	writeLogFile(t, dir, "2026-09-30.log", rec("2026-09-30T00:00:00Z", "ERROR", "yesterday"))
	writeGzipLog(t, dir, "2026-08-20.log.gz", rec("2026-08-20T00:00:00Z", "ERROR", "archived"))
	writeLogFile(t, dir, "2026-08-19.log", rec("2026-08-19T00:00:00Z", "ERROR", "too-old"))
	e := newTestExporter(dir)

	if got := msgs(t, export(t, e, 1, slog.LevelError)); strings.Join(got, ",") != "today" {
		t.Errorf("days=1 = %v, want only today", got)
	}
	if got := msgs(t, export(t, e, 7, slog.LevelError)); strings.Join(got, ",") != "yesterday,today" {
		t.Errorf("days=7 = %v, want yesterday,today in time order", got)
	}
	// 2026-08-20 and 2026-08-19 are 42 and 43 days before 2026-10-01: only
	// the 90-day window reaches them.
	if got := msgs(t, export(t, e, 90, slog.LevelError)); strings.Join(got, ",") != "too-old,archived,yesterday,today" {
		t.Errorf("days=90 = %v", got)
	}
	if got := msgs(t, export(t, e, 30, slog.LevelError)); strings.Join(got, ",") != "yesterday,today" {
		t.Errorf("days=30 = %v, want the 42-day-old files excluded", got)
	}
}

func TestExporter_PrefersPlainFileOverArchiveOfSameDay(t *testing.T) {
	dir := t.TempDir()
	writeLogFile(t, dir, "2026-10-01.log", rec("2026-10-01T00:00:00Z", "ERROR", "plain"))
	writeGzipLog(t, dir, "2026-10-01.log.gz", rec("2026-10-01T00:00:00Z", "ERROR", "archive"))

	got := msgs(t, export(t, newTestExporter(dir), 1, slog.LevelError))
	if strings.Join(got, ",") != "plain" {
		t.Errorf("messages = %v, want the plain file only (an interrupted archival leaves both)", got)
	}
}

func TestExporter_OrdersByRecordTimeAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	// The 23:59 record was written just after midnight, into the next file.
	writeLogFile(t, dir, "2026-10-01.log",
		rec("2026-09-30T23:59:59.9Z", "ERROR", "late-yesterday")+rec("2026-10-01T00:00:01Z", "ERROR", "today"))
	writeLogFile(t, dir, "2026-09-30.log", rec("2026-09-30T12:00:00Z", "ERROR", "noon"))

	got := msgs(t, export(t, newTestExporter(dir), 2, slog.LevelError))
	if strings.Join(got, ",") != "noon,late-yesterday,today" {
		t.Errorf("messages = %v", got)
	}
}

func TestExporter_NoMatchesIsEmptyNotError(t *testing.T) {
	res := export(t, newTestExporter(t.TempDir()), 7, slog.LevelError)
	if len(res.Data) != 0 || res.Records != 0 || res.Truncated {
		t.Errorf("result = %+v, want empty and not truncated", res)
	}
}

func TestExporter_UnreadableDirectoryIsError(t *testing.T) {
	e := newTestExporter(filepath.Join(t.TempDir(), "missing"))
	if _, err := e.Export(context.Background(), 7, slog.LevelError); err == nil {
		t.Fatal("Export on a missing log directory succeeded, want error")
	}
}

func TestExporter_CorruptArchiveIsError(t *testing.T) {
	dir := t.TempDir()
	writeLogFile(t, dir, "2026-10-01.log.gz", "this is not gzip")
	if _, err := newTestExporter(dir).Export(context.Background(), 1, slog.LevelError); err == nil {
		t.Fatal("Export with a corrupt archive succeeded, want error")
	}
}

func TestExporter_DoesNotModifyLogs(t *testing.T) {
	dir := t.TempDir()
	original := rec("2026-10-01T00:00:00Z", "ERROR", "boom") + `{"level":"ERROR","msg":"x","password":"hunter2"}` + "\n"
	writeLogFile(t, dir, "2026-10-01.log", original)

	export(t, newTestExporter(dir), 1, slog.LevelError)

	data, err := os.ReadFile(filepath.Join(dir, "2026-10-01.log"))
	if err != nil || string(data) != original {
		t.Errorf("log file after export = %q (err %v), want it unchanged", data, err)
	}
}

func TestExporter_TruncatesOldestFirstWhenOverCap(t *testing.T) {
	dir := t.TempDir()
	var yesterday, today strings.Builder
	for i := range 4 {
		yesterday.WriteString(rec(fmt.Sprintf("2026-09-30T00:00:0%dZ", i), "ERROR", fmt.Sprintf("y%d", i)))
		today.WriteString(rec(fmt.Sprintf("2026-10-01T00:00:0%dZ", i), "ERROR", fmt.Sprintf("t%d", i)))
	}
	writeLogFile(t, dir, "2026-09-30.log", yesterday.String())
	writeLogFile(t, dir, "2026-10-01.log", today.String())

	e := newTestExporter(dir)
	lineSize := len(rec("2026-10-01T00:00:00Z", "ERROR", "t0"))
	e.maxBytes = 6*lineSize + lineSize/2 // room for six records

	res := export(t, e, 2, slog.LevelError)
	if got := msgs(t, res); strings.Join(got, ",") != "y2,y3,t0,t1,t2,t3" {
		t.Errorf("messages = %v, want the newest six", got)
	}
	if !res.Truncated {
		t.Error("Truncated = false, want true")
	}
	if len(res.Data) > e.maxBytes {
		t.Errorf("len(Data) = %d > cap %d", len(res.Data), e.maxBytes)
	}

	e.maxBytes = MaxExportBytes
	if res := export(t, e, 2, slog.LevelError); res.Truncated || res.Records != 8 {
		t.Errorf("under the cap: Records=%d Truncated=%v, want 8 and false", res.Records, res.Truncated)
	}
}

func TestExporter_MasksSecrets(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		`{"time":"2026-10-01T00:00:01Z","level":"ERROR","msg":"keys","api_key":"k1","Password":"p","X-Api-Key":"k2","nested":{"token":{"deep":"d"},"ok":"visible"},"list":[{"secret":1},{"fine":2}],"webhook_url":"https://hooks.slack.com/services/T0/B0/xyz"}`,
		`{"time":"2026-10-01T00:00:02Z","level":"ERROR","msg":"post to https://hooks.slack.com/services/T000/B000/AbCdEf failed","error":"Get \"https://api.example/x?token=abc123&q=1&apikey=k&password=pw\": dial tcp: lookup failed","hdr":"Authorization: Bearer eyJhbGciOi.abc-def_ghi== end"}`,
		`{"time":"2026-10-01T00:00:03Z","level":"ERROR","msg":"plain \u003c&\u003e","n":12345678901234567890,"f":1.50,"b":true,"z":null}`,
	}
	writeLogFile(t, dir, "2026-10-01.log", strings.Join(lines, "\n")+"\n")

	res := export(t, newTestExporter(dir), 1, slog.LevelError)
	out := strings.Split(strings.TrimSuffix(string(res.Data), "\n"), "\n")
	if len(out) != 3 {
		t.Fatalf("got %d lines: %s", len(out), res.Data)
	}

	wantKeys := `{"time":"2026-10-01T00:00:01Z","level":"ERROR","msg":"keys","api_key":"[REDACTED]","Password":"[REDACTED]","X-Api-Key":"[REDACTED]","nested":{"token":"[REDACTED]","ok":"visible"},"list":[{"secret":"[REDACTED]"},{"fine":2}],"webhook_url":"[REDACTED]"}`
	if out[0] != wantKeys {
		t.Errorf("key masking:\n got %s\nwant %s", out[0], wantKeys)
	}

	for _, secret := range []string{"T000/B000", "abc123", "apikey=k&", "pw", "eyJhbGciOi", "Bearer eyJ"} {
		if strings.Contains(out[1], secret) {
			t.Errorf("secret %q survived: %s", secret, out[1])
		}
	}
	for _, want := range []string{`post to [REDACTED] failed`, `?token=[REDACTED]&q=1&apikey=[REDACTED]&password=[REDACTED]`, `dial tcp: lookup failed`, `Authorization: Bearer [REDACTED] end`} {
		if !strings.Contains(out[1], want) {
			t.Errorf("line 2 missing %q: %s", want, out[1])
		}
	}

	wantPlain := `{"time":"2026-10-01T00:00:03Z","level":"ERROR","msg":"plain <&>","n":12345678901234567890,"f":1.50,"b":true,"z":null}`
	if out[2] != wantPlain {
		t.Errorf("untouched record changed:\n got %s\nwant %s", out[2], wantPlain)
	}
}
