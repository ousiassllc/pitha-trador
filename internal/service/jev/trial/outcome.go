package trial

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
	"github.com/ousiassllc/pitha-trador/internal/textutil"
)

// traderQuestionCount is the number of questions of the FR-TRADER-1 group; a
// response that refused all of them is StatusRefused.
const traderQuestionCount = 6

// TruncationSuspectBytesPerToken is the request-bytes-per-input-token ratio above
// which a row is marked truncation_suspected. The request is JSON of numbers
// plus Japanese text, which tokenizes at about 2-4 bytes a token; a provider
// that reports far fewer input tokens than the request's size suggests may
// have cut the input (e.g. to a smaller context window). The ratio is an
// unmeasured SUSPICION marker, not a verdict: the row also keeps
// request_bytes, state_bytes and input_tokens so the judgement can be made
// later from the material.
const TruncationSuspectBytesPerToken = 6.0

// callResult is the outcome of one arm call.
type callResult struct {
	resp         trialwire.Response
	latency      time.Duration
	requestBytes int
	err          error
}

// armInput is what the row records about the request, the same for every arm.
type armInput struct {
	live       int64
	stateBytes int
	ragFlag    bool
}

// ragCarriesLiveAnswers reports whether the RAG context sent with a request has
// any past case: those cases carry the live model's past answers, so the arm
// is not a fully independent condition (rag_includes_live_model_answers).
// It is deliberately conservative: a context with any case is flagged.
func ragCarriesLiveAnswers(c rag.Context) bool { return len(c.Cases) > 0 }

// buildDecision turns one call's outcome into the row to store, and the cost it
// was counted at. Answers stay nil where the model refused or reported none.
func buildDecision(arm jevtrialsettings.Arm, in armInput, r callResult, now time.Time) jevtrial.Decision {
	d := jevtrial.Decision{
		JevDecisionID:               in.live,
		ArmID:                       arm.ID,
		PromptVersion:               arm.PromptVersion,
		Provider:                    arm.Provider,
		RequestedModel:              arm.Model,
		LatencyMs:                   int(r.latency.Milliseconds()),
		RequestBytes:                r.requestBytes,
		StateBytes:                  in.stateBytes,
		LLMCutoffStatus:             jevtrial.CutoffUnverified,
		RAGIncludesLiveModelAnswers: in.ragFlag,
		CreatedAt:                   now.UTC(),
	}
	if r.err != nil {
		fillFailure(&d, r)
		return d
	}
	fillAnswer(&d, arm, r)
	return d
}

func fillAnswer(d *jevtrial.Decision, arm jevtrialsettings.Arm, r callResult) {
	resp := r.resp
	switch n := len(resp.Refused); {
	case n == 0:
		d.Status = jevtrial.StatusOK
	case n >= traderQuestionCount:
		d.Status = jevtrial.StatusRefused
	default:
		d.Status = jevtrial.StatusPartial
	}
	d.ServedModel, d.ServedProvider, d.ResponseID = resp.ModelID, resp.Provider, resp.ResponseID
	d.Direction, d.Regime, d.EntryQuality = nonEmpty(resp.Direction), nonEmpty(resp.Regime), nonEmpty(resp.EntryQuality)
	d.Confidence, d.ToxicFlow = resp.Confidence, resp.ToxicFlow
	d.LiquidityStressed, d.ContinuationProbability = resp.LiquidityStressed, resp.ContinuationProbability
	if resp.Body != "" {
		d.ResponseJSON = new(resp.Body)
	}
	if resp.InputTokens > 0 {
		d.InputTokens = new(resp.InputTokens)
	}
	if resp.OutputTokens > 0 {
		d.OutputTokens = new(resp.OutputTokens)
	}
	d.TruncationSuspected = truncationSuspected(r.requestBytes, resp.InputTokens)
	c := AnsweredCost(arm, resp, r.requestBytes)
	d.CostUSD, d.CostSource, d.CountedCostUSD = c.USD, c.Source, c.CountedUSD
}

func fillFailure(d *jevtrial.Decision, r callResult) {
	d.Status = jevtrial.StatusFailed
	var status *int
	d.FailureCode, status = classifyFailure(r.err)
	d.HTTPStatus = status
	d.ErrorExcerpt = textutil.Excerpt(errorText(r.err), jevtrial.MaxErrorExcerptBytes)
	c := FailedCost(status != nil, r.requestBytes)
	d.CostUSD, d.CostSource, d.CountedCostUSD = c.USD, c.Source, c.CountedUSD
}

// errorText is the text an error excerpt is cut from: an API error's body (the
// vendor's explanation), else the error's own message.
func errorText(err error) string {
	var apiErr *jev.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Body
	}
	return err.Error()
}

// classifyFailure maps a call error to its failure code and, for an HTTP error
// answer, the status.
func classifyFailure(err error) (code string, status *int) {
	var apiErr *jev.APIError
	if errors.As(err, &apiErr) {
		return httpFailureCode(apiErr.StatusCode), new(apiErr.StatusCode)
	}
	var netErr net.Error
	switch {
	case errors.Is(err, systemone.ErrInvalidResponse):
		return jevtrial.FailureInvalidResponse, nil
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return jevtrial.FailureTimeout, nil
	}
	return jevtrial.FailureTransport, nil
}

func httpFailureCode(status int) string {
	switch status {
	case http.StatusPaymentRequired:
		return jevtrial.FailureHTTP402
	case http.StatusForbidden:
		return jevtrial.FailureHTTP403
	case http.StatusRequestEntityTooLarge:
		return jevtrial.FailureHTTP413
	case http.StatusTooManyRequests:
		return jevtrial.FailureHTTP429
	case 529:
		return jevtrial.FailureHTTP529
	}
	switch {
	case status >= 500:
		return jevtrial.FailureHTTP5xx
	case status >= 400:
		return jevtrial.FailureHTTP4xx
	}
	// A status below 400 that is not 200 is not an error answer of the API.
	return jevtrial.FailureTransport
}

// truncationSuspected marks a response whose reported input tokens are far below
// what its request's size suggests (TruncationSuspectBytesPerToken).
func truncationSuspected(requestBytes, inputTokens int) bool {
	return inputTokens > 0 && float64(requestBytes)/float64(inputTokens) > TruncationSuspectBytesPerToken
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return new(s)
}
