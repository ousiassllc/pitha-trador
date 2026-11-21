package logging

import (
	"log/slog"
	"strings"
	"testing"
)

// #736 acceptance: none of the Tachibana secrets reach the error-log export
// (GET /api/v1/logs/errors serves Exporter.Export), whether they sit in an
// attribute or in an error string.
func TestExporter_MasksTachibanaSecrets(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		`{"time":"2026-10-01T00:00:01Z","level":"ERROR","msg":"tachibana login failed","sAuthId":"AUTH-SECRET","private_key":"PEM-SECRET","second":"SECOND-SECRET","sUrlRequest":"URL-SECRET-1","virtual_url":"URL-SECRET-2"}`,
		`{"time":"2026-10-01T00:00:02Z","level":"ERROR","msg":"request failed","error":"Post \"https://kabuka.e-shiten.jp/e_api_v4r10/request/MjAyNjEwMDgwMDAwMDBUT0tFTjEyMzQ1Ng/?sAuthId=AUTH-SECRET\": EOF"}`,
		`{"time":"2026-10-01T00:00:03Z","level":"ERROR","msg":"key rejected","error":"parse -----BEGIN PRIVATE KEY-----\nPEM-BODY-SECRET\n-----END PRIVATE KEY-----"}`,
	}
	writeLogFile(t, dir, "2026-10-01.log", strings.Join(lines, "\n")+"\n")

	res := export(t, newTestExporter(dir), 1, slog.LevelError)
	if res.Records != 3 {
		t.Fatalf("Records = %d, want 3: %s", res.Records, res.Data)
	}
	for _, secret := range []string{"AUTH-SECRET", "PEM-SECRET", "SECOND-SECRET", "URL-SECRET", "MjAyNjEwMDgw", "PEM-BODY-SECRET"} {
		if strings.Contains(string(res.Data), secret) {
			t.Errorf("secret %q survived the export:\n%s", secret, res.Data)
		}
	}
}
