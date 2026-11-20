package mask_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote/mask"
)

// Text a researcher writes (URLs, paths, identifiers, UUIDs, host names, plain
// English) must not turn into [masked].
func TestTextKeepsReadableLongWords(t *testing.T) {
	for _, in := range []string{
		"https://github.com/ousiassllc/pitha-trador/issues/1095",
		"https://github.com/ousiassllc/pitha-trador/pull/1105#issuecomment-123456789",
		"db/migrations/000067_create_research_notes",
		"internal/service/paperresultsmcp/researchnote/mask/mask_words.go",
		"jev_evaluation_horizon_30m_cohort_llm_vs_baseline",
		"123e4567-e89b-12d3-a456-426614174000",
		"123E4567-E89B-12D3-A456-426614174000",
		"sagemaker.us-east-1.amazonaws.com",
		"runtime.sagemaker.us-east-1.amazonaws.com/endpoints/jev-30m/invocations",
		"2026-11-15T10:00:00.123456789+09:00",
		"the basic approach",
		"a Basic idea and a Digest of results, the bearer of news",
		"see /var/lib/pitha/data/research_notes_2026_11_15_backup.sqlite3",
	} {
		if got := mask.Text(in); got != in {
			t.Errorf("Text(%q) = %q, want it unchanged", in, got)
		}
	}
}

func TestTextMasksOpaqueWords(t *testing.T) {
	hex := strings.Repeat("0123456789abcdef", 3)
	for in, want := range map[string]string{
		"hash " + hex + " end": "hash [masked] end",
		"base64url aB3dE5gH7jK-mN1pQ3sT5vW7yZ9bD_fH3jL5nP7rT9u end":         "base64url [masked] end",
		"base64 dGhpcyBpcyBh+IHZlcnkgbG9u/ZyBvcGFxdWUgc2VjcmV0IGJvZHk= end": "base64 [masked] end",
		"https://example.com/share/" + hex:                                  "https://example.com/share/[masked]",
		"/var/lib/data/" + hex + "/file":                                    "/var/lib/data/[masked]/file",
		"abcd1234efgh.ijkl5678mnop.qrst9012uvwx":                            "[masked]",
		"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig":                      "jwt [masked]",
	} {
		if got := mask.Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTextMasksPrefixedKeysAfterWordCharacters(t *testing.T) {
	sendgrid := "SG." + strings.Repeat("a", 22) + "." + strings.Repeat("B", 43)
	for in, want := range map[string]string{
		"foo_sk-abcdefghijklmnop1234":                "foo_[masked]",
		"x=ghp_abcdefghijklmnopqrstuvwxyz0123456789": "x=[masked]",
		"key " + sendgrid + " end":                   "key [masked] end",
		"xoxp-1234567890":                            "[masked]",
		"slack xoxs-abcdefghij1 end":                 "slack [masked] end",
		"xoxb-123":                                   "xoxb-123",
		"task-force-abcdefghijklmnop":                "task-force-abcdefghijklmnop",
		"SG.short.value":                             "SG.short.value",
	} {
		if got := mask.Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

// Unicode format characters (zero-width space / joiner, word joiner, BOM,
// soft hyphen) are removed so they cannot split a credential.
func TestTextRemovesFormatCharactersBeforeMasking(t *testing.T) {
	for in, want := range map[string]string{
		"sk-\u200bproj-abcdefghijklmnopqrstuv0123456789": "[masked]",
		"Bearer abc\u200b123":                            "Bearer [masked]",
		"pass\u2060word=hunter2":                         "password=[masked]",
		"\ufeffapi_key=abc\u00ad123":                     "api_key=[masked]",
		"plain\u200b text":                               "plain text",
	} {
		if got := mask.Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTextBearerWordIsMaskedOnlyWithACredential(t *testing.T) {
	checkTable(t, map[string]string{
		"curl -H 'Bearer tok_12345'": "curl -H 'Bearer [masked]'",
		"BEARER abcdef":              "BEARER [masked]",
		"bearer abc123 end":          "bearer [masked] end",
		"a bearer of news":           "a bearer of news",
	})
}
