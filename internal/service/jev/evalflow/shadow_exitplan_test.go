package evalflow_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/shadow"
)

// Issue #925: trader-v5 (the exit plan wording) runs in the shadow beside the
// live trader-v3 and nothing else.

func enableV5(e *shadowEnv) {
	e.set(jevshadowsettings.KeyEnabled, "1")
	e.set(jevshadowsettings.KeyCandidateVersion, `"trader-v5"`)
}

// splitRequest returns a wire request's questions and every other top-level
// field (the state and the RAG context).
func splitRequest(t *testing.T, body []byte) (questions, rest map[string]json.RawMessage) {
	t.Helper()
	if err := json.Unmarshal(body, &rest); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rest["questions"], &questions); err != nil {
		t.Fatalf("request without questions: %v", err)
	}
	delete(rest, "questions")
	return questions, rest
}

func TestShadowV5_CallsWithTheLiveInputAndRecordsTokensCostAndEstimateFlag(t *testing.T) {
	for name, noUsage := range map[string]bool{"usage reported": false, "no usage, estimated": true} {
		t.Run(name, func(t *testing.T) {
			api := &fakeJev{noUsageShadow: noUsage}
			e := newShadowEnv(t, metalabelsettings.ModePaper, api)
			enableV5(e)
			live := e.evaluate()
			e.shadow.Wait()

			if len(api.bodies["trader-v3"]) != 1 || len(api.bodies["trader-v5"]) != 1 || len(api.bodies) != 2 {
				t.Fatalf("requests by version = %d v3, %d v5, %d versions; want one v3 (live) and one v5 (shadow)", len(api.bodies["trader-v3"]), len(api.bodies["trader-v5"]), len(api.bodies))
			}
			liveQ, liveRest := splitRequest(t, api.bodies["trader-v3"][0])
			shadowQ, shadowRest := splitRequest(t, api.bodies["trader-v5"][0])
			if len(liveRest) == 0 || !reflect.DeepEqual(liveRest, shadowRest) {
				t.Errorf("the shadow's input differs from the live one:\nlive   %s\nshadow %s", liveRest, shadowRest)
			}
			for id, q := range liveQ {
				changed := id == "direction" || id == "continuation_probability"
				if same := string(q) == string(shadowQ[id]); same == changed {
					t.Errorf("question %q equal to the live one = %v, want %v", id, same, !changed)
				}
			}

			var version string
			var tokens, estimate int
			var cost float64
			if err := e.db.QueryRow(`SELECT prompt_version, input_tokens, request_cost, cost_is_estimate FROM jev_shadow_decisions WHERE jev_decision_id = ?`, live.ID).
				Scan(&version, &tokens, &cost, &estimate); err != nil {
				t.Fatal(err)
			}
			wantTokens, wantEstimate := 1200, 0
			if noUsage {
				wantTokens, wantEstimate = shadow.EstimateInputTokens(api.shadowBytes[0]), 1
			}
			if version != "trader-v5" || tokens != wantTokens || estimate != wantEstimate || tokens <= 0 || cost != shadow.InputCostUSD(tokens) || cost <= 0 {
				t.Errorf("row = %s tokens=%d cost=%v estimate=%d; want trader-v5, %d tokens, their price (>0), estimate=%d", version, tokens, cost, estimate, wantTokens, wantEstimate)
			}
		})
	}
}

// The live decision and the live request are the same whether the trader-v5
// shadow is on or off, and the shadow reaches no promotion or live setting.
func TestShadowV5_LivePathIsUnchangedByTheShadow(t *testing.T) {
	off := newShadowEnv(t, metalabelsettings.ModePaper, &fakeJev{})
	on := newShadowEnv(t, metalabelsettings.ModePaper, &fakeJev{})
	enableV5(on)
	liveOff, liveOn := off.evaluate(), on.evaluate()
	on.shadow.Wait()

	if on.api.calls("trader-v5") != 1 || off.api.calls("trader-v5") != 0 {
		t.Fatalf("v5 calls: shadow on %d, off %d; want 1 and 0", on.api.calls("trader-v5"), off.api.calls("trader-v5"))
	}
	if string(off.api.bodies["trader-v3"][0]) != string(on.api.bodies["trader-v3"][0]) {
		t.Error("the live request differs with the shadow on")
	}
	if liveOn.QuestionVersion != "trader-v3" || liveOn.QuestionVersion != liveOff.QuestionVersion || liveOn.ResponseJSON != liveOff.ResponseJSON ||
		liveOn.EntryQuality == nil || liveOff.EntryQuality == nil || *liveOn.EntryQuality != *liveOff.EntryQuality ||
		liveOn.Direction == nil || liveOff.Direction == nil || *liveOn.Direction != *liveOff.Direction ||
		liveOn.ContinuationProbability == nil || liveOff.ContinuationProbability == nil || *liveOn.ContinuationProbability != *liveOff.ContinuationProbability {
		t.Errorf("live decision with the shadow on %+v, off %+v", liveOn, liveOff)
	}
	if n := on.count(`SELECT COUNT(*) FROM jev_decisions WHERE question_version <> 'trader-v3'`); n != 0 {
		t.Errorf("%d jev_decisions rows are not trader-v3", n)
	}
	if n := on.count(`SELECT COUNT(*) FROM runtime_settings WHERE key = ?`, jevshadowsettings.KeyLiveVersion); n != 0 {
		t.Errorf("the live version setting was written (%d rows): only a promotion may", n)
	}
	if n := on.count(`SELECT COUNT(*) FROM jev_prompt_promotions`); n != 0 {
		t.Errorf("jev_prompt_promotions has %d rows after a shadow comparison: the shadow never promotes", n)
	}

}
