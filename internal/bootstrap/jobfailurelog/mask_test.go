package jobfailurelog_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/jobfailurelog"
)

func TestMaskMessage_HidesSecretsAndRawData(t *testing.T) {
	tests := []struct {
		name, in string
		leaked   []string
		kept     []string
	}{
		{
			name:   "bearer and query credentials",
			in:     "jev: api error (status 401): Authorization: Bearer abc.DEF-ghi and https://api.example.com/v1?api_key=KEYVALUE&x=1",
			leaked: []string{"abc.DEF-ghi", "KEYVALUE", "api.example.com"},
			kept:   []string{"jev: api error (status 401)"},
		},
		{
			name:   "tachibana virtual url and auth id",
			in:     `tachibana: login: sAuthId=AUTHSECRET https://demo-kabuka.e-shiten.jp/e_api_v4r5/SESSIONTOKENSESSIONTOKEN123/`,
			leaked: []string{"AUTHSECRET", "SESSIONTOKEN"},
		},
		{
			name:   "long digit runs (order id, account, price, volume)",
			in:     "reject order 1234567890 account 00123456 price 12345.5 qty 100",
			leaked: []string{"1234567890", "00123456", "12345"},
			kept:   []string{"qty 100"},
		},
		{
			name:   "opaque token",
			in:     "bad key sk-AbCdEf0123456789AbCdEf0123456789 given",
			leaked: []string{"AbCdEf0123456789"},
			kept:   []string{"bad key", "given"},
		},
		{
			name:   "embedded JSON body (board data)",
			in:     `decode board: {"Symbol":"7203","CurrentPrice":3050.5,"Buy1":{"Price":3050,"Qty":300}} trailing`,
			leaked: []string{"CurrentPrice", "3050", "Buy1", "7203"},
			kept:   []string{"decode board:"},
		},
		{
			name:   "truncated JSON body",
			in:     `decode board: {"Symbol":"7203","CurrentPr`,
			leaked: []string{"Symbol", "7203"},
		},
		{
			name:   "JSON array body",
			in:     `decode ranking: [{"Symbol":"6758"},{"Symbol":"9984"}]`,
			leaked: []string{"6758", "9984", "Symbol"},
		},
		{
			name:   "pem key",
			in:     "parse: -----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBg\n-----END PRIVATE KEY-----",
			leaked: []string{"MIIEvQIBADANBg"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := jobfailurelog.MaskMessage(tc.in)
			for _, s := range tc.leaked {
				if strings.Contains(got, s) {
					t.Errorf("MaskMessage leaked %q: %q", s, got)
				}
			}
			for _, s := range tc.kept {
				if !strings.Contains(got, s) {
					t.Errorf("MaskMessage dropped %q: %q", s, got)
				}
			}
		})
	}
}

func TestMaskMessage_SingleLineAndBounded(t *testing.T) {
	in := "first line\n\tsecond   line " + strings.Repeat("あ", 500)
	got := jobfailurelog.MaskMessage(in)
	if strings.ContainsAny(got, "\n\t") || strings.Contains(got, "  ") {
		t.Errorf("MaskMessage not collapsed to one line: %q", got)
	}
	if len(got) > jobfailurelog.MaxMessageBytes || !utf8.ValidString(got) {
		t.Errorf("MaskMessage len=%d valid=%v, want <= %d and valid UTF-8", len(got), utf8.ValidString(got), jobfailurelog.MaxMessageBytes)
	}
	if !strings.HasPrefix(got, "first line second line") {
		t.Errorf("MaskMessage = %q, want the leading text kept", got)
	}
	// An enormous message is cut before the regexps run.
	huge := strings.Repeat("x ", 5<<20)
	if got := jobfailurelog.MaskMessage(huge); len(got) > jobfailurelog.MaxMessageBytes {
		t.Errorf("MaskMessage(huge) len = %d, want <= %d", len(got), jobfailurelog.MaxMessageBytes)
	}
}
