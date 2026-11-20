package mask_test

import (
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote/mask"
)

// fullwidth writes the printable ASCII of s as fullwidth forms (U+FF01..U+FF5E)
// and its spaces as U+3000, the way an IME types it.
func fullwidth(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '!' && r <= '~':
			return r + 0xFEE0
		case r == ' ':
			return '　'
		}
		return r
	}, s)
}

// A key name, a prefix or a scheme typed in fullwidth is masked like its
// halfwidth spelling; only the secret is replaced, the key and everything else
// stay as the caller wrote them (fullwidth and all).
func TestTextMasksFullwidthKeyNamesAndPrefixes(t *testing.T) {
	checkTable(t, map[string]string{
		"ＡＰＩ_ＫＥＹ＝abc123":                       "ＡＰＩ_ＫＥＹ＝[masked]",
		"ＡＰＩ＿ＫＥＹ：ｈｕｎｔｅｒ２ next":                 "ＡＰＩ＿ＫＥＹ：[masked] next",
		"ｐａｓｓｗｏｒｄ＝ｐ＠ｓｓ１":                       "ｐａｓｓｗｏｒｄ＝[masked]",
		"ｘ－ａｐｉ－ｋｅｙ: ａｂｃ１２３ tail":               "ｘ－ａｐｉ－ｋｅｙ: [masked] tail",
		"ｓｋ－ａｂｃｄｅｆｇｈ１２３４５ end":                 "[masked] end",
		"memo ｓｋ－ｐｒｏｊ－ａｂｃｄｅｆｇｈ１２３４ end":        "memo [masked] end",
		"ｇｈｐ＿ａｂｃｄｅｆｇｈｉｊｋｌｍｎｏｐｑｒｓｔ０１２３":         "[masked]",
		"ＡＫＩＡＩＯＳＦＯＤＮＮ７ＥＸＡＭＰＬＥ":                 "[masked]",
		"memo Ｂｅａｒｅｒ　ｔｏｋ＿１２３４５ end":            "memo Ｂｅａｒｅｒ　[masked] end",
		"Ａｕｔｈｏｒｉｚａｔｉｏｎ：　Ｂｅａｒｅｒ　ａｂｃ１２３":         "Ａｕｔｈｏｒｉｚａｔｉｏｎ：　[masked]",
		"ｔｏｋｅｎ　ａｂｃ１２３４５ｘｙｚ":                    "ｔｏｋｅｎ　[masked]",
		"ｈｔｔｐｓ：／／ｕｓｅｒ：ｓ３ｃｒ３ｔ＠ｅｘａｍｐｌｅ．ｃｏｍ／ｐａｔｈ": "ｈｔｔｐｓ：／／ｕｓｅｒ：[masked]＠ｅｘａｍｐｌｅ．ｃｏｍ／ｐａｔｈ",
	})
}

// The text around a masked span is returned untouched, even when folding the
// text before the secret changes its length (① -> 1, ㎏ -> kg, ™ -> TM).
func TestTextKeepsTheTextAroundAFullwidthSecret(t *testing.T) {
	checkTable(t, map[string]string{
		"①㎏™ ＡＰＩ_ＫＥＹ＝ｘ９８７６５４３２ 後ろ ＡＢＣ（１２３）": "①㎏™ ＡＰＩ_ＫＥＹ＝[masked] 後ろ ＡＢＣ（１２３）",
		"api_key=ab㎏12345 tail": "api_key=[masked] tail",
		"㎏㎏ ｓｋ－ａｂｃｄｅｆｇｈ１２３４５ ㎏㎏":              "㎏㎏ [masked] ㎏㎏",
		"日本語の説明。ＡＰＩ＿ＫＥＹ＝ｓｅｃ１２３４，続き ＡＰＩ_ＫＥＹ＝ｚ": "日本語の説明。ＡＰＩ＿ＫＥＹ＝[masked]，続き ＡＰＩ_ＫＥＹ＝[masked]",
		"ｐａｓｓｗｏｒｄ＝\"ａ ｂ　ｃ\" 後":                "ｐａｓｓｗｏｒｄ＝\"[masked]\" 後",
	})
}

// Ordinary Japanese and the examples that stay readable stay readable, in
// fullwidth too.
func TestTextKeepsOrdinaryFullwidthText(t *testing.T) {
	for _, in := range []string{
		"出来高（％）が１２３円、ＡＢＣ：説明。ＮＹＳＥ・ＮＡＳＤＡＱ",
		"ｍａｘ＿ｔｏｋｅｎｓ＝４０９６　ｔｏｋｅｎ＿ｃｏｕｎｔ：１２",
		"ｈｔｔｐｓ：／／ｇｉｔｈｕｂ．ｃｏｍ／ｏｕｓｉａｓｓｌｌｃ／ｐｉｔｈａ－ｔｒａｄｏｒ／ｉｓｓｕｅｓ／１０９５",
		"２０２６－１１－１５Ｔ１０：００：００Ｚ　２０２６－１１－１５Ｔ１０：００：００＋０９：００",
		"ｐａｓｓ＿ｒａｔｅ：０．８　ｐａｓｓｗｏｒｄ　ｐｏｌｉｃｙ　を比べる",
		"認証は不要。パスワードは別の話。①㎏™…",
		"ｂｅａｒｅｒ　ｔｏｋｅｎ　ｂｕｄｇｅｔ",
	} {
		if got := mask.Text(in); got != in {
			t.Errorf("Text(%q) = %q, want it unchanged", in, got)
		}
	}
}

// Every halfwidth secret and assignment of the other tests, typed in
// fullwidth, is masked exactly where the halfwidth spelling is (compared on the
// NFKC form, so the fullwidth text outside the mask does not matter), and none
// of its secret characters survives.
func TestFullwidthSpellingIsMaskedLikeTheHalfwidthOne(t *testing.T) {
	for _, in := range []string{
		"memo sk-proj-abcdefghijklmnopqrstuv0123456789 end",
		"memo ghp_abcdefghijklmnopqrstuvwxyz0123456789 end",
		"memo AKIAIOSFODNN7EXAMPLE end",
		"memo eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk end",
		"memo 0123456789abcdef0123456789abcdef01234567 end",
		"memo abcd1234efgh.ijkl5678mnop.qrst9012uvwx end",
		"api_key=abc123 end",
		"API-KEY: hunter2 next",
		`{"password": "p@ss w0rd"}`,
		"Authorization: Bearer abcDEF123._-xyz",
		"curl -H 'Bearer tok_12345' x",
		"url https://user:s3cr3t@example.com/path",
		"?token=zzz&b=2",
		"client_secret = \"abc\"",
		"token abc12345xyz",
		"Bearer abc.def and ghp_abcdefghijklmnopqrstuvwxyz0123456789 and password=Zm9vYmFyYmF6",
	} {
		fw := fullwidth(in)
		got := mask.Text(fw)
		if want := mask.Text(in); norm.NFKC.String(got) != norm.NFKC.String(want) {
			t.Errorf("Text(%q) = %q, want the masks of %q (%q)", fw, got, in, want)
		}
		if !mask.Contains(fw) || mask.Contains(got) {
			t.Errorf("Contains(%q) = %v, Contains(%q) = %v", fw, mask.Contains(fw), got, mask.Contains(got))
		}
	}
}
