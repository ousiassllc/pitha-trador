package mask_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote/mask"
)

// The secret-looking strings below are made up for the test (none is a real credential).
func TestTextMasksKeyShapedStrings(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	for name, secret := range map[string]string{
		"openai key":        "sk-proj-abcdefghijklmnopqrstuv0123456789",
		"anthropic key":     "sk-ant-api03-ABCDEFGHIJKLMNOP_qrstuvwx-0123",
		"github token":      "ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"github fine token": "github_pat_11ABCDEFG0abcdefghijkl_mnopqrstuvwxyz",
		"slack token":       "xoxb-123456789012-abcdefghij",
		"aws access key id": "AKIAIOSFODNN7EXAMPLE",
		"google key":        "AIzaSyA-abcdefghijklmnopqrstuvwxyz012345",
		"jwt":               jwt,
		"hex 40":            "0123456789abcdef0123456789abcdef01234567",
		"base64 body":       "dGhpcyBpcyBhIHZlcnkgbG9uZyBvcGFxdWUgc2VjcmV0IGJvZHk",
		"dotted token":      "abcd1234efgh.ijkl5678mnop.qrst9012uvwx",
		"stripe live":       "sk_live_abcdefghijklmnop0123",
	} {
		in := "memo " + secret + " end"
		got := mask.Text(in)
		if strings.Contains(got, secret) || !strings.Contains(got, mask.Replacement) || !strings.HasPrefix(got, "memo ") || !strings.HasSuffix(got, " end") {
			t.Errorf("%s: Text(%q) = %q", name, in, got)
		}
	}
}

func TestTextMasksCredentialAssignmentsAndAuthorization(t *testing.T) {
	for in, want := range map[string]string{
		"api_key=abc123":                           "api_key=[masked]",
		"API-KEY: hunter2 next":                    "API-KEY: [masked] next",
		`{"password": "p@ss w0rd"}`:                `{"password": "[masked] w0rd"}`,
		"password=short&x=1":                       "password=[masked]&x=1",
		"Authorization: Bearer abcDEF123._-xyz":    "Authorization: [masked]",
		"curl -H 'Bearer tok_12345'":               "curl -H 'Bearer [masked]'",
		"url https://user:s3cr3t@example.com/path": "url https://user:[masked]@example.com/path",
		"?token=zzz&b=2":                           "?token=[masked]&b=2",
		"client_secret = \"abc\"":                  "client_secret = \"[masked]\"",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIx\n-----END RSA PRIVATE KEY----- after": "[masked] after",
	} {
		got := mask.Text(in)
		if got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTextKeepsOrdinaryResearchText(t *testing.T) {
	for _, in := range []string{
		"",
		"出来高が増えると 30 分後のリターンが改善するか",
		"experiment_id 12 と backtest_result_id 345 を比べた。純損益は +1.5%（95% 区間 0.2〜2.8）",
		"https://example.com/docs/research-notes",
		"internal/service/paperresultsmcp/researchnote/mask",
		"risk.paper.max_trade_loss_pct を 1 から 0.5 に",
		"2026-11-15T10:00:00Z",
		"password policy を比べる話ではない",
		"supercalifragilisticexpialidocious_is_a_long_but_plain_word",
	} {
		if got := mask.Text(in); got != in {
			t.Errorf("Text(%q) = %q, want it unchanged", in, got)
		}
	}
}

func TestTextIsIdempotent(t *testing.T) {
	in := "key sk-abcdefghijklmnop1234 and api_key=zzz and Bearer abc.def and eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig"
	once := mask.Text(in)
	if twice := mask.Text(once); twice != once {
		t.Errorf("Text is not idempotent: %q -> %q", once, twice)
	}
	if !mask.Contains(in) || mask.Contains(once) {
		t.Errorf("Contains: in=%v once=%v", mask.Contains(in), mask.Contains(once))
	}
}

func TestTextsMasksEachElement(t *testing.T) {
	got := mask.Texts([]string{"plain", "ghp_abcdefghijklmnopqrstuvwxyz0123456789"})
	if len(got) != 2 || got[0] != "plain" || got[1] != mask.Replacement {
		t.Errorf("Texts = %q", got)
	}
}
