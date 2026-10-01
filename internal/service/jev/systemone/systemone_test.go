package systemone_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

func TestChoiceQuestion_EncodesOptionsInDeclarationOrder(t *testing.T) {
	q := systemone.ChoiceQuestion("rate it",
		systemone.Option{Name: "weak", Rubric: "w"},
		systemone.Option{Name: "moderate", Rubric: "m"},
		systemone.Option{Name: "strong", Rubric: "s"},
	)
	raw, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"type":"choice","instructions":"rate it","criteria":{"weak":"w","moderate":"m","strong":"s"}}`
	if string(raw) != want {
		t.Errorf("question JSON = %s, want %s", raw, want)
	}
}

func TestNoulQuestion_EncodesTrueFalseCriteria(t *testing.T) {
	raw, err := json.Marshal(systemone.NoulQuestion("is it?", "yes means", "no means"))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"type":"noul","instructions":"is it?","criteria":{"true":"yes means","false":"no means"}}`
	if string(raw) != want {
		t.Errorf("question JSON = %s, want %s", raw, want)
	}
}

func TestDecode_IgnoresExtraAnswersAndReportsUsage(t *testing.T) {
	req := systemone.Request{Questions: map[string]systemone.Question{
		"a": systemone.NoulQuestion("a?", "y", "n"),
	}}
	body := `{"model":"jev-1.13.0","answers":{"a":{"type":"noul","noul":1},"extra":{"type":"score","score":3}},"usage":{"input_tokens":10,"output_tokens":2}}`

	got, err := systemone.Decode(req, []byte(body))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Noul("a") != 1 || got.Model != "jev-1.13.0" || got.Usage != (systemone.Usage{InputTokens: 10, OutputTokens: 2}) {
		t.Errorf("Decode() = %+v, want noul 1, model jev-1.13.0 and usage 10/2", got)
	}
}

func TestDecode_RejectsMissingModel(t *testing.T) {
	req := systemone.Request{Questions: map[string]systemone.Question{"a": systemone.NoulQuestion("a?", "y", "n")}}
	_, err := systemone.Decode(req, []byte(`{"answers":{"a":{"type":"noul","noul":0.5}}}`))
	if err == nil || !strings.Contains(err.Error(), "missing model") {
		t.Errorf("Decode error = %v, want missing model", err)
	}
}
