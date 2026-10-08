package logging

import (
	"log/slog"
	"strings"
	"testing"
)

const (
	tachibanaVirtualURL = "https://kabuka.e-shiten.jp/e_api_v4r10/request/MjAyNjEwMDgwMDAwMDBUT0tFTjEyMzQ1Ng/"
	tachibanaPEM        = "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASC\nKgwggSoAgEAAoIBAQC7\n-----END PRIVATE KEY-----"
)

// 立花証券 e支店API (#736): 認証ID・秘密鍵・第二暗証番号・仮想URL are masked
// by attribute key, including ones nested in groups.
func TestMaskJSON_TachibanaCredentialKeys(t *testing.T) {
	in := `{"msg":"login","authid":"AUTH1","auth_id":"AUTH2","sAuthId":"AUTH3","AuthID":"AUTH4",` +
		`"private_key":"PEM1","privateKey":"PEM2","second":"SEC1","second_password":"SEC2","sSecondPassword":"SEC3",` +
		`"surl":"u1","sUrlRequest":"u2","sUrlEventWebSocket":"u3","virtual_url":"u4","virtualUrl":"u5",` +
		`"tachibana":{"auth_id":{"deep":"AUTH5"},"ok":"visible"}}`
	got, err := maskJSON([]byte(in))
	if err != nil {
		t.Fatalf("maskJSON: %v", err)
	}
	for _, secret := range []string{"AUTH1", "AUTH2", "AUTH3", "AUTH4", "AUTH5", "PEM1", "PEM2", "SEC1", "SEC2", "SEC3", `"u1"`, `"u2"`, `"u3"`, `"u4"`, `"u5"`} {
		if strings.Contains(string(got), secret) {
			t.Errorf("secret %s survived: %s", secret, got)
		}
	}
	if !strings.Contains(string(got), `"ok":"visible"`) {
		t.Errorf("unrelated nested value was masked: %s", got)
	}
}

// A "second" substring must not hide ordinary duration attributes, and
// "surl" must stay a prefix match.
func TestMaskJSON_TachibanaKeysDoNotOverMatch(t *testing.T) {
	in := `{"msg":"x","elapsed_seconds":3,"timeout_second_count":2,"seconds":5,"base_url":"https://kabuka.e-shiten.jp/e_api_v4r10/","insurl":"v"}`
	got, err := maskJSON([]byte(in))
	if err != nil {
		t.Fatalf("maskJSON: %v", err)
	}
	if string(got) != in {
		t.Errorf("non-secret keys changed:\n got %s\nwant %s", got, in)
	}
}

func TestMaskString_TachibanaCredentials(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"virtual url", "Post " + tachibanaVirtualURL + " failed", "Post [REDACTED] failed"},
		{"virtual url quoted by url.Error", `Post \"` + tachibanaVirtualURL + `\": EOF`, `Post \"[REDACTED]\": EOF`},
		{"virtual url master upper-case host", "HTTPS://KABUKA.E-SHITEN.JP/e_api_v4r10/master/MjAyNjEwMDgwMDAwMDBUT0tFTjEyMzQ1Ng/ x", "[REDACTED] x"},
		{"virtual url websocket", "dial wss://kabuka.e-shiten.jp/e_api_v4r10/event/MjAyNjEwMDgwMDAwMDBUT0tFTjEyMzQ1Ng/?p_rid=22 EOF", "dial [REDACTED] EOF"},
		{"demo virtual url", "https://demo-kabuka.e-shiten.jp/e_api_v4r10/price/MjAyNjEwMDgwMDAwMDBUT0tFTjEyMzQ1Ng/", "[REDACTED]"},
		{"login url is public", "Post https://kabuka.e-shiten.jp/e_api_v4r10/auth/: EOF", "Post https://kabuka.e-shiten.jp/e_api_v4r10/auth/: EOF"},
		{"authid query", "https://x/y?sAuthId=AUTH1&q=1", "https://x/y?sAuthId=[REDACTED]&q=1"},
		{"auth_id query", "https://x/y?auth_id=AUTH1&q=1", "https://x/y?auth_id=[REDACTED]&q=1"},
		{"second password query", "https://x/y?sSecondPassword=SEC1&q=1", "https://x/y?sSecondPassword=[REDACTED]&q=1"},
		{"second query", "https://x/y?second=SEC1&q=1", "https://x/y?second=[REDACTED]&q=1"},
		{"seconds query untouched", "https://x/y?seconds=5&q=1", "https://x/y?seconds=5&q=1"},
		{"surl query", "https://x/y?sUrlRequest=u1&q=1", "https://x/y?sUrlRequest=[REDACTED]&q=1"},
		{"virtual_url query", "https://x/y?virtual_url=u1&q=1", "https://x/y?virtual_url=[REDACTED]&q=1"},
		{"private_key query", "https://x/y?private_key=PEM1&q=1", "https://x/y?private_key=[REDACTED]&q=1"},
		{"json body", `request {"p_no":"1","sAuthId":"AUTH1","sJsonOfmt":"5"}`, `request {"p_no":"1","sAuthId":"[REDACTED]","sJsonOfmt":"5"}`},
		{"json second password", `{"sSecondPassword":"SEC1","x":"y"}`, `{"sSecondPassword":"[REDACTED]","x":"y"}`},
		{"json escaped quotes", `Get \"https://x/y?{\"sAuthId\":\"AUTH1\",\"p_no\":\"1\"}\": EOF`, `Get \"https://x/y?{\"sAuthId\":\"[REDACTED]\",\"p_no\":\"1\"}\": EOF`},
		{"json percent-encoded", "https://x/y?%7B%22sAuthId%22%3A%22AUTH1%22%2C%22p_no%22%3A%221%22%7D", "https://x/y?%7B%22sAuthId%22%3A%22[REDACTED]%22%2C%22p_no%22%3A%221%22%7D"},
		{"json virtual url value", `{"sUrlRequest":"` + tachibanaVirtualURL + `","x":"y"}`, `{"sUrlRequest":"[REDACTED]","x":"y"}`},
		{"pem private key", "parse key: " + tachibanaPEM + " rejected", "parse key: [REDACTED] rejected"},
		{"pem rsa private key", "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----", "[REDACTED]"},
		{"pem unterminated", "k: -----BEGIN PRIVATE KEY-----\nMIIEvQ", "k: [REDACTED]"},
		{"public key untouched", "-----BEGIN PUBLIC KEY-----\nMIIB\n-----END PUBLIC KEY-----", "-----BEGIN PUBLIC KEY-----\nMIIB\n-----END PUBLIC KEY-----"},
		{"plain text untouched", "auth id is not configured; second step failed", "auth id is not configured; second step failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maskString(tt.in); got != tt.want {
				t.Errorf("maskString(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

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
