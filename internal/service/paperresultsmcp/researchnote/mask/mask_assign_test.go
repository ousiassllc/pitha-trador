package mask_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote/mask"
)

func checkTable(t *testing.T, table map[string]string) {
	t.Helper()
	for in, want := range table {
		if got := mask.Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

// A value that opens with a quote is masked up to the closing quote; an
// unquoted one up to the next white space, quote or comma, whatever it holds.
func TestTextMasksWholeValues(t *testing.T) {
	checkTable(t, map[string]string{
		`password = "correct horse battery staple"`:  `password = "[masked]"`,
		`pwd='two words' next`:                       `pwd='[masked]' next`,
		`{"password": "p@ss w0rd"}`:                  `{"password": "[masked]"}`,
		`{"password": "a \"quoted\" word"} tail`:     `{"password": "[masked]"} tail`,
		`password: P@ss&w0rd!#1;x next`:              `password: [masked] next`,
		`password=a;b@c!d#e,rest`:                    `password=[masked],rest`,
		`token: "unterminated value` + "\nnext line": `token: "[masked]` + "\nnext line",
		`password=short&x=1`:                         `password=[masked]&x=1`,
		`secret=abc&def next`:                        `secret=[masked] next`,
		`token: Bearer abc123 after`:                 `token: [masked] after`,
		`auth: basic dXNlcjpwYXNz after`:             `auth: [masked] after`,
		`{\"api_key\": \"abc def\", \"n\": 1}`:       `{\"api_key\": \"[masked]\", \"n\": 1}`,
		`{"api_key":"abc"}`:                          `{"api_key":"[masked]"}`,
		"password:   \nnext":                         "password:   \nnext",
	})
}

func TestTextMasksAuthorizationToEndOfLine(t *testing.T) {
	checkTable(t, map[string]string{
		"Authorization: Bearer abc def\nnext":              "Authorization: [masked]\nnext",
		"Authorization: Basic dXNlcjpwYXNz":                "Authorization: [masked]",
		"Authorization: Token abc.def ghi":                 "Authorization: [masked]",
		"Authorization: ApiKey k-1 v 2":                    "Authorization: [masked]",
		"Authorization: Digest username=x, response=y":     "Authorization: [masked]",
		"Authorization: opaque-value with words":           "Authorization: [masked]",
		"curl -H 'Authorization: Bearer abc' https://x/y":  "curl -H 'Authorization: [masked]' https://x/y",
		`curl -H "Authorization: Basic x y" --url z`:       `curl -H "Authorization: [masked]" --url z`,
		"Proxy-Authorization = hunter2 and more":           "Proxy-Authorization = [masked]",
		`{"Authorization": "Digest user=\"x\", nc=1"} end`: `{"Authorization": "[masked]"} end`,
	})
}

func TestTextMasksTheWiderKeyVocabulary(t *testing.T) {
	for _, key := range []string{
		"pass", "passwd", "pw", "pwd", "access_key", "secret_key", "private_key", "cookie", "session", "session_id", "sessionId",
		"signature", "auth", "credential", "credentials", "OPENAI_API_KEY", "x-api-key", "apiKey", "db.password", "client_secret",
		"access_token", "SECRET_KEY_BASE",
	} {
		for _, sep := range []string{"=", ": ", " = ", "："} {
			in := "a " + key + sep + "hunter2 b"
			if got, want := mask.Text(in), "a "+key+sep+"[masked] b"; got != want {
				t.Errorf("Text(%q) = %q, want %q", in, got, want)
			}
		}
	}
}

func TestTextMasksJapaneseKeys(t *testing.T) {
	checkTable(t, map[string]string{
		"パスワード: hunter2 です":     "パスワード: [masked] です",
		"トークン＝abc123":           "トークン＝[masked]",
		"秘密鍵：xyz":               "秘密鍵：[masked]",
		"秘密=abc":                "秘密=[masked]",
		"APIキー=abc":             "APIキー=[masked]",
		"API キー: abc":           "API キー: [masked]",
		"認証: abc":               "認証: [masked]",
		"パスワードは Abc12345xyz です": "パスワードは [masked] です",
		// は is ordinary prose unless the value looks opaque (8+ characters with a digit).
		"認証は不要":         "認証は不要",
		"パスワードは変更しない":   "パスワードは変更しない",
		"トークンは abc で十分": "トークンは abc で十分",
	})
}

func TestTextMasksSpacedKeyWithOpaqueValue(t *testing.T) {
	checkTable(t, map[string]string{
		"token abc12345xyz":              "token [masked]",
		"my secret Abcdef12345 here":     "my secret [masked] here",
		"the password hunter2024x ok":    "the password [masked] ok",
		"access_token abc12345xyz":       "access_token [masked]",
		"token budget is large":          "token budget is large",
		"token 12":                       "token 12",
		"password policy を比べる":           "password policy を比べる",
		"secret abcdefghij has no digit": "secret abcdefghij has no digit",
	})
}

// Ordinary key names must stay readable: only a key that is a credential word
// itself (as a whole word or the last part of the name) masks its value.
func TestTextKeepsOrdinaryKeyNames(t *testing.T) {
	for _, in := range []string{
		"max_tokens: 4096", "max_tokens=4096", "token_count: 12345", "tokens: 12", "pass_rate: 0.8", "bypass: yes",
		"author: foo", "session_count: 3", "authority=x", "compass: north", "keyword: value", "key: value",
	} {
		if got := mask.Text(in); got != in {
			t.Errorf("Text(%q) = %q, want it unchanged", in, got)
		}
	}
}
