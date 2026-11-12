package trialflow_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

func TestDefaultSettings_NothingRuns(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	a := e.addArm("a1", answer(okResp(), 3000))
	live := e.evaluate()

	if live.ID == 0 {
		t.Fatal("the live decision must still be made")
	}
	if a.calls() != 0 || e.count(`SELECT COUNT(*) FROM jev_trial_decisions`) != 0 || e.settings.setCount() != 0 {
		t.Fatalf("calls=%d rows=%d settings writes=%d, want 0 with the default settings",
			a.calls(), e.count(`SELECT COUNT(*) FROM jev_trial_decisions`), e.settings.setCount())
	}
	// Enabled but no arms: still nothing. Arms but switched off: nothing.
	e.set(jevtrialsettings.KeyEnabled, "1")
	e.evaluate()
	e.enable(defaultArm("a1"))
	e.set(jevtrialsettings.KeyEnabled, "0")
	e.evaluate()
	// A damaged switch is off, never on.
	e.set(jevtrialsettings.KeyEnabled, `"yes"`)
	e.evaluate()
	if a.calls() != 0 || e.settings.setCount() != 0 {
		t.Fatalf("calls=%d settings writes=%d, want 0", a.calls(), e.settings.setCount())
	}
}

func TestLiveMode_DoesNothing(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModeLive, nil)
	a := e.addArm("a1", answer(okResp(), 3000))
	e.enable(defaultArm("a1"))
	if live := e.evaluate(); live.ID == 0 {
		t.Fatal("the live decision must still be made in Live mode")
	}
	if a.calls() != 0 || e.count(`SELECT COUNT(*) FROM jev_trial_decisions`) != 0 {
		t.Fatal("the trial ran in Live mode")
	}
}

func TestArmsRunInParallelOnTheSameInputAndNeverSeeEachOther(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	var arrived atomic.Int32
	barrier := make(chan struct{})
	rendezvous := func(resp trialwire.Response) func(context.Context, jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
		return func(ctx context.Context, _ jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
			if arrived.Add(1) == 2 {
				close(barrier)
			}
			select {
			case <-barrier:
				return resp, time.Millisecond, 3000, nil
			case <-time.After(10 * time.Second): // a bound only: serial arms never meet
				return trialwire.Response{}, 0, 0, errors.New("the other arm was never called in parallel")
			}
		}
	}
	short := okResp()
	short.Direction = "SHORT"
	a1 := e.addArm("a1", rendezvous(okResp()))
	a2 := e.addArm("a2", rendezvous(short))
	e.enable(defaultArm("a1"), defaultArm("a2"))

	live := e.evaluate()
	r1, r2 := e.rowOf("a1"), e.rowOf("a2")
	if r1.Status != jevtrial.StatusOK || r2.Status != jevtrial.StatusOK {
		t.Fatalf("statuses %q / %q, want ok (both arms must run at once)", r1.Status, r2.Status)
	}
	if r1.JevDecisionID != live.ID || r2.JevDecisionID != live.ID {
		t.Errorf("rows link to %d / %d, want live decision %d", r1.JevDecisionID, r2.JevDecisionID, live.ID)
	}
	if *r1.Direction != "LONG" || *r2.Direction != "SHORT" {
		t.Errorf("directions %s / %s: each arm keeps its own answer", *r1.Direction, *r2.Direction)
	}
	// Both received the identical state, RAG context and version, nothing else.
	q1, q2 := a1.reqs[0], a2.reqs[0]
	if fmt.Sprintf("%+v", q1) != fmt.Sprintf("%+v", q2) || q1.QuestionVersion != "trader-v3" || q1.State.Symbol != "7203" {
		t.Errorf("arm inputs differ: %+v vs %+v", q1, q2)
	}
}

func TestStatusMappingAndNullAnswers(t *testing.T) {
	partial := okResp()
	partial.Refused, partial.ToxicFlow = []string{"toxic_flow"}, nil
	refused := trialwire.Response{
		Refused: []string{"continuation_probability", "direction", "entry_quality", "liquidity_stressed", "regime", "toxic_flow"},
		ModelID: "served-model", InputTokens: 900, Body: `{"refused":true}`,
	}
	noConfidence := okResp()
	noConfidence.Confidence = nil
	for name, tc := range map[string]struct {
		resp   trialwire.Response
		status string
	}{
		"ok": {okResp(), jevtrial.StatusOK}, "partial": {partial, jevtrial.StatusPartial},
		"refused": {refused, jevtrial.StatusRefused}, "ok without confidence": {noConfidence, jevtrial.StatusOK},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, metalabelsettings.ModePaper, nil)
			e.addArm("a1", answer(tc.resp, 3000))
			e.enable(defaultArm("a1"))
			e.evaluate()
			d := e.rowOf("a1")
			if d.Status != tc.status || d.FailureCode != "" || d.HTTPStatus != nil {
				t.Fatalf("status=%q failure=%q http=%v, want %q", d.Status, d.FailureCode, d.HTTPStatus, tc.status)
			}
			if d.ResponseJSON == nil || *d.ResponseJSON != tc.resp.Body || d.ServedModel != "served-model" {
				t.Errorf("response_json=%v served_model=%q", d.ResponseJSON, d.ServedModel)
			}
			switch tc.status {
			case jevtrial.StatusRefused:
				if d.Direction != nil || d.Regime != nil || d.EntryQuality != nil || d.Confidence != nil ||
					d.ToxicFlow != nil || d.LiquidityStressed != nil || d.ContinuationProbability != nil {
					t.Errorf("a refused row must have NULL answers (never 0/NONE): %+v", d)
				}
			case jevtrial.StatusPartial:
				if d.ToxicFlow != nil || d.Direction == nil || *d.Direction != "LONG" {
					t.Errorf("partial row: toxic_flow=%v direction=%v", d.ToxicFlow, d.Direction)
				}
			default:
				if (name == "ok without confidence") != (d.Confidence == nil) {
					t.Errorf("confidence = %v for %q", d.Confidence, name)
				}
			}
		})
	}
}

func TestFailuresAreRowsWithACodeAndABoundedExcerpt(t *testing.T) {
	longBody := strings.Repeat("secret-looking error body ", 400)
	for name, tc := range map[string]struct {
		err    error
		code   string
		status int
	}{
		"402":        {&jev.APIError{StatusCode: 402, Body: longBody}, jevtrial.FailureHTTP402, 402},
		"403":        {&jev.APIError{StatusCode: 403, Body: longBody}, jevtrial.FailureHTTP403, 403},
		"413":        {&jev.APIError{StatusCode: 413, Body: longBody}, jevtrial.FailureHTTP413, 413},
		"429":        {&jev.APIError{StatusCode: 429, Body: longBody}, jevtrial.FailureHTTP429, 429},
		"529":        {&jev.APIError{StatusCode: 529, Body: longBody}, jevtrial.FailureHTTP529, 529},
		"400":        {&jev.APIError{StatusCode: 400, Body: longBody}, jevtrial.FailureHTTP4xx, 400},
		"500":        {&jev.APIError{StatusCode: 500, Body: longBody}, jevtrial.FailureHTTP5xx, 500},
		"timeout":    {fmt.Errorf("trialwire: request: %w", context.DeadlineExceeded), jevtrial.FailureTimeout, 0},
		"transport":  {errors.New("dial tcp: connection refused"), jevtrial.FailureTransport, 0},
		"invalid":    {fmt.Errorf("decode: %w", systemone.ErrInvalidResponse), jevtrial.FailureInvalidResponse, 0},
		"wrapped500": {fmt.Errorf("wrapped: %w", &jev.APIError{StatusCode: 502, Body: "bad gateway"}), jevtrial.FailureHTTP5xx, 502},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, metalabelsettings.ModePaper, nil)
			e.addArm("a1", fail(tc.err, 3000))
			e.enable(defaultArm("a1"))
			e.evaluate()
			d := e.rowOf("a1")
			if d.Status != jevtrial.StatusFailed || d.FailureCode != tc.code {
				t.Fatalf("status=%q failure_code=%q, want failed/%s", d.Status, d.FailureCode, tc.code)
			}
			if (tc.status == 0) != (d.HTTPStatus == nil) || (d.HTTPStatus != nil && *d.HTTPStatus != tc.status) {
				t.Errorf("http_status = %v, want %d", d.HTTPStatus, tc.status)
			}
			if len(d.ErrorExcerpt) == 0 || len(d.ErrorExcerpt) > jevtrial.MaxErrorExcerptBytes {
				t.Errorf("error_excerpt has %d bytes, want 1..%d", len(d.ErrorExcerpt), jevtrial.MaxErrorExcerptBytes)
			}
			if d.Direction != nil || d.ResponseJSON != nil || d.CostUSD != nil || d.CostSource != jevtrial.CostUnknown {
				t.Errorf("a failed row has answers/cost: %+v", d)
			}
			if d.RequestedModel != "cloudflare/a1" || d.RequestBytes != 3000 || d.StateBytes == 0 {
				t.Errorf("request material missing: model=%q request_bytes=%d state_bytes=%d", d.RequestedModel, d.RequestBytes, d.StateBytes)
			}
		})
	}
}

func TestRAGFlagCutoffAndTruncationMaterial(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	e.addArm("a1", answer(okResp(), 3000))
	e.enable()
	live := e.evaluate() // trial off: the live decision only
	e.enable(defaultArm("a1"))
	withCase := rag.Context{Cases: []rag.SimilarCase{{Source: "jev_decision", Symbol: "7203", Direction: new("LONG")}}}
	e.trial.Submit(live, state(e.clock()), withCase)
	e.trial.Wait()
	d := e.rowOf("a1")
	if !d.RAGIncludesLiveModelAnswers {
		t.Error("a row sent RAG cases must have rag_includes_live_model_answers = true")
	}
	if d.LLMCutoffStatus != jevtrial.CutoffUnverified {
		t.Errorf("llm_cutoff_status = %q, want unverified", d.LLMCutoffStatus)
	}
	if d.TruncationSuspected || d.InputTokens == nil || *d.InputTokens != 1200 || d.RequestBytes != 3000 {
		t.Errorf("truncation_suspected=%v input_tokens=%v request_bytes=%d", d.TruncationSuspected, d.InputTokens, d.RequestBytes)
	}

	// No cases: false. Few reported tokens for a big request: suspicion, nothing more.
	small := okResp()
	small.InputTokens = 100
	e2 := newEnv(t, metalabelsettings.ModePaper, nil)
	e2.addArm("a1", answer(small, 4000))
	e2.enable(defaultArm("a1"))
	e2.evaluate() // the first live decision has no similar past case
	d2 := e2.rowOf("a1")
	if d2.RAGIncludesLiveModelAnswers {
		t.Error("an empty RAG context must give rag_includes_live_model_answers = false")
	}
	if !d2.TruncationSuspected || d2.Status != jevtrial.StatusOK {
		t.Errorf("truncation_suspected=%v status=%q: a suspicion marks the row, it does not drop it", d2.TruncationSuspected, d2.Status)
	}
}

func TestArmWithoutKeyOrUnregisteredVersionIsNeverCalled(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	a := e.addArm("nokey", answer(okResp(), 3000))
	a.configured = false
	bad := e.addArm("badver", answer(okResp(), 3000))
	armBad := defaultArm("badver")
	armBad.PromptVersion = "trader-v999"
	// "absent" has no caller at all.
	e.enable(defaultArm("nokey"), armBad, defaultArm("absent"))
	e.evaluate()
	if a.calls() != 0 || bad.calls() != 0 || e.count(`SELECT COUNT(*) FROM jev_trial_decisions`) != 0 {
		t.Fatal("an arm without a key, caller or registered prompt version was called or stored")
	}
}

func TestPanicInAnArmIsRecoveredAndTheLiveDecisionIsUnaffected(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	e.addArm("boom", func(context.Context, jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
		panic("arm exploded")
	})
	e.addArm("fine", answer(okResp(), 3000))
	e.enable(defaultArm("boom"), defaultArm("fine"))
	live := e.evaluate()
	if live.ID == 0 || e.count(`SELECT COUNT(*) FROM jev_decisions`) != 1 {
		t.Fatalf("the live decision was affected: %+v", live)
	}
	if rows := e.rows(); len(rows) != 1 || rows[0].ArmID != "fine" {
		t.Fatalf("rows = %+v, want the healthy arm's row only", rows)
	}
}
