package researchnote

import (
	"strings"
	"testing"
)

func TestCleanDefaultsTrimsMasksAndNormalisesTags(t *testing.T) {
	f, err := Fields{
		Question: "  何が効くか  ", Hypothesis: "key sk-abcdefghijklmnop1234 here",
		Tags:          []string{" Volume ", "volume", "AKIAIOSFODNN7EXAMPLE", "ghp_abcdefghijklmnopqrstuvwxyz0123456789"},
		EvidenceLinks: []Evidence{{Kind: KindURL, Ref: " https://example.com/x "}},
	}.Clean()
	if err != nil {
		t.Fatal(err)
	}
	if f.Question != "何が効くか" || f.Conclusion != ConclusionPending || f.Hypothesis != "key [masked] here" {
		t.Errorf("fields = %+v", f)
	}
	if got := strings.Join(f.Tags, ","); got != "volume,[masked]" {
		t.Errorf("tags = %q, want trimmed, lower-cased, de-duplicated and masked (the two masked tags collapse)", got)
	}
	if f.EvidenceLinks[0].Ref != "https://example.com/x" {
		t.Errorf("evidence = %+v", f.EvidenceLinks)
	}
}

func TestCleanRefusesWithoutQuotingValues(t *testing.T) {
	secret := "sk-abcdefghijklmnop1234"
	tooMany := make([]string, MaxTags+1)
	for i := range tooMany {
		tooMany[i] = "t"
	}
	for name, f := range map[string]Fields{
		"empty question": {Question: " "},
		"long question":  {Question: strings.Repeat("あ", MaxQuestionRunes+1)},
		"long actions":   {Question: "q", Actions: strings.Repeat("a", MaxActionsRunes+1) + secret},
		"control":        {Question: "q\x01" + secret},
		"conclusion":     {Question: "q", Conclusion: secret},
		"kind":           {Question: "q", EvidenceLinks: []Evidence{{Kind: secret, Ref: "r"}}},
		"empty ref":      {Question: "q", EvidenceLinks: []Evidence{{Kind: KindOther}}},
		"too many tags":  {Question: "q", Tags: tooMany},
		"too many links": {Question: "q", EvidenceLinks: make([]Evidence, MaxEvidenceLinks+1)},
		"long tag":       {Question: "q", Tags: []string{strings.Repeat("t", MaxTagRunes+1)}},
	} {
		_, err := f.Clean()
		reason, ok := IsInvalid(err)
		if !ok || strings.Contains(reason, secret) {
			t.Errorf("%s: err = %v (invalid=%v), want a reason that does not quote the value", name, err, ok)
		}
	}
}

// A mask can be longer than the text it replaces; the stored value must still fit its limit.
func TestCleanChecksTheLengthAfterMasking(t *testing.T) {
	text := strings.Repeat("password=a ", 270) // 2970 characters, 270 * len("password=[masked] ") = 4590 after
	_, err := Fields{Question: "q", ResultSummary: text}.Clean()
	if reason, ok := IsInvalid(err); !ok || !strings.Contains(reason, "伏せ字") {
		t.Errorf("err = %v, want a refusal that names the mask", err)
	}
}

func TestPatchKeepsTheFieldsItDoesNotName(t *testing.T) {
	base := Fields{Question: "q", Hypothesis: "h", Conclusion: ConclusionPending, Tags: []string{"a"}}
	s, none := "new", []string{}
	got := Patch{ResultSummary: &s, Tags: &none}.apply(base)
	if got.Question != "q" || got.Hypothesis != "h" || got.ResultSummary != "new" || len(got.Tags) != 0 {
		t.Errorf("patched = %+v", got)
	}
	if !(Patch{}).empty() || (Patch{Tags: &none}).empty() {
		t.Error("empty() is wrong")
	}
}

func TestEscapeLike(t *testing.T) {
	if got := escapeLike(`50%_a\b`); got != `50\%\_a\\b` {
		t.Errorf("escapeLike = %q", got)
	}
}

func TestExcerptCutsAtTheLimit(t *testing.T) {
	if got := excerpt(strings.Repeat("あ", listExcerptRunes)); strings.Contains(got, "…") {
		t.Error("a text at the limit was cut")
	}
	if got := []rune(excerpt(strings.Repeat("あ", listExcerptRunes+1))); len(got) != listExcerptRunes+1 || got[listExcerptRunes] != '…' {
		t.Errorf("excerpt = %d runes", len(got))
	}
}
