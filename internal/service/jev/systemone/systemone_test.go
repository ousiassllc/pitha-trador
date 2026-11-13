package systemone_test

import (
	"encoding/json"
	"errors"
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

func choiceReq() systemone.Request {
	return systemone.Request{Questions: map[string]systemone.Question{
		"dir":  systemone.ChoiceQuestion("which?", systemone.Option{Name: "LONG", Rubric: "l"}, systemone.Option{Name: "SHORT", Rubric: "s"}),
		"flag": systemone.NoulQuestion("is it?", "y", "n"),
	}}
}

const trialFixture = `{
  "id": "gen-dec-1789738314-X5e5eKGQdvR9rblyX250",
  "model": "cloudflare/clef-flash-20261001",
  "provider": "DeepInfra",
  "answers": {
    "dir":  {"type": "choice", "choice": "SHORT", "probabilities": {"LONG": 0.31, "SHORT": 0.69}},
    "flag": {"type": "refusal"}
  },
  "usage": {"input_tokens": 120, "output_tokens": 4, "cost": 0.0000046}
}`

func TestDecodeTrial_ReadsRefusalOptionalConfidenceProbabilitiesCostProviderAndID(t *testing.T) {
	got, err := systemone.DecodeTrial(choiceReq(), []byte(trialFixture))
	if err != nil {
		t.Fatalf("DecodeTrial: %v", err)
	}
	if !got.Refused("flag") || got.Refused("dir") || got.RefusedCount() != 1 {
		t.Errorf("refusals = flag:%v dir:%v count:%d, want only flag refused", got.Refused("flag"), got.Refused("dir"), got.RefusedCount())
	}
	dir := got.Choice("dir")
	if dir.Value != "SHORT" || dir.ConfidenceReported || dir.Confidence != 0 {
		t.Errorf("dir = %+v, want SHORT with no reported confidence", dir)
	}
	if dir.Probabilities["SHORT"] != 0.69 || dir.Probabilities["LONG"] != 0.31 {
		t.Errorf("dir probabilities = %v, want the raw 0.31/0.69", dir.Probabilities)
	}
	if got.ID != "gen-dec-1789738314-X5e5eKGQdvR9rblyX250" || got.Provider != "DeepInfra" || got.Model != "cloudflare/clef-flash-20261001" {
		t.Errorf("id/provider/model = %q/%q/%q", got.ID, got.Provider, got.Model)
	}
	if got.ReportedCostUSD == nil || *got.ReportedCostUSD != 0.0000046 {
		t.Errorf("ReportedCostUSD = %v, want 0.0000046", got.ReportedCostUSD)
	}
	if got.Body != trialFixture {
		t.Errorf("Body is not the raw response")
	}
}

func TestDecodeTrial_AbsentOrMistypedCostIsNotReportedNotZero(t *testing.T) {
	for name, usage := range map[string]string{
		"absent":   `{"input_tokens":1,"output_tokens":1}`,
		"string":   `{"input_tokens":1,"output_tokens":1,"cost":"0.1"}`,
		"negative": `{"input_tokens":1,"output_tokens":1,"cost":-1}`,
		"null":     `{"input_tokens":1,"output_tokens":1,"cost":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"model":"m","answers":{"dir":{"type":"choice","choice":"LONG"},"flag":{"type":"noul","noul":0.5}},"usage":` + usage + `}`
			got, err := systemone.DecodeTrial(choiceReq(), []byte(body))
			if err != nil {
				t.Fatalf("DecodeTrial: %v", err)
			}
			if got.ReportedCostUSD != nil {
				t.Errorf("ReportedCostUSD = %v, want nil (not reported)", *got.ReportedCostUSD)
			}
		})
	}
}

// The live path's strict validation is unchanged: everything DecodeTrial
// accepts above is still an invalid response for Decode (issue #1058).
func TestDecode_StaysStrictOnRefusalAndMissingConfidence(t *testing.T) {
	for name, body := range map[string]string{
		"refusal":            `{"model":"m","answers":{"dir":{"type":"refusal"},"flag":{"type":"noul","noul":0.5}}}`,
		"missing confidence": `{"model":"m","answers":{"dir":{"type":"choice","choice":"LONG"},"flag":{"type":"noul","noul":0.5}}}`,
		"refused noul":       `{"model":"m","answers":{"dir":{"type":"choice","choice":"LONG","confidence":0.5},"flag":{"type":"refusal"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := systemone.Decode(choiceReq(), []byte(body)); !errors.Is(err, systemone.ErrInvalidResponse) {
				t.Errorf("Decode error = %v, want ErrInvalidResponse", err)
			}
		})
	}
}

func TestDecode_DoesNotReadTrialFields(t *testing.T) {
	got, err := systemone.Decode(choiceReq(), []byte(`{"id":"gen-1","provider":"P","model":"m","answers":{"dir":{"type":"choice","choice":"LONG","confidence":0.5,"probabilities":{"LONG":1}},"flag":{"type":"noul","noul":0.5}},"usage":{"cost":0.5}}`))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.ID != "" || got.Provider != "" || got.ReportedCostUSD != nil || got.Body != "" || got.Choice("dir").Probabilities != nil {
		t.Errorf("Decode read trial-only fields: %+v", got)
	}
	// A mistyped trial-only field is not a reason to fail the strict path either.
	if _, err := systemone.Decode(choiceReq(), []byte(`{"id":7,"model":"m","answers":{"dir":{"type":"choice","choice":"LONG","confidence":0.5},"flag":{"type":"noul","noul":0.5}},"usage":{"cost":"x"}}`)); err != nil {
		t.Errorf("Decode with mistyped id/cost: %v", err)
	}
}

func TestDecodeTrial_StillRejectsInvalidAnswerSets(t *testing.T) {
	for name, body := range map[string]string{
		"missing answer":       `{"model":"m","answers":{"flag":{"type":"noul","noul":0.5}}}`,
		"wrong type":           `{"model":"m","answers":{"dir":{"type":"noul","noul":0.5},"flag":{"type":"noul","noul":0.5}}}`,
		"choice not an option": `{"model":"m","answers":{"dir":{"type":"choice","choice":"NONE"},"flag":{"type":"noul","noul":0.5}}}`,
		"confidence range":     `{"model":"m","answers":{"dir":{"type":"choice","choice":"LONG","confidence":1.5},"flag":{"type":"noul","noul":0.5}}}`,
		"noul missing":         `{"model":"m","answers":{"dir":{"type":"choice","choice":"LONG"},"flag":{"type":"noul"}}}`,
		"missing model":        `{"answers":{"dir":{"type":"choice","choice":"LONG"},"flag":{"type":"noul","noul":0.5}}}`,
		"not json":             `nope`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := systemone.DecodeTrial(choiceReq(), []byte(body)); !errors.Is(err, systemone.ErrInvalidResponse) {
				t.Errorf("DecodeTrial error = %v, want ErrInvalidResponse", err)
			}
		})
	}
}

func TestRequest_ProviderIsOmittedUnlessSet(t *testing.T) {
	plain, err := json.Marshal(systemone.Request{State: "s", Model: "m", Questions: map[string]systemone.Question{}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "provider") || strings.Contains(string(plain), "data_collection") || strings.Contains(string(plain), "zdr") {
		t.Errorf("default request carries routing fields: %s", plain)
	}

	no, yes := false, true
	routed, err := json.Marshal(systemone.Request{State: "s", Model: "m", Questions: map[string]systemone.Question{}, Provider: &systemone.Provider{
		Only: []string{"deepinfra"}, AllowFallbacks: &no, DataCollection: "deny", ZDR: &yes,
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := `"provider":{"only":["deepinfra"],"allow_fallbacks":false,"data_collection":"deny","zdr":true}`
	if !strings.Contains(string(routed), want) {
		t.Errorf("routed request = %s, want it to contain %s", routed, want)
	}
}
