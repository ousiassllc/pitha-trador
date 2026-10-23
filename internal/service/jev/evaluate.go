package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/httpbody"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// HTTP statuses the API documents for backoff-and-retry (429 rate
// limited, 529 overloaded).
const (
	statusRateLimited = http.StatusTooManyRequests
	statusOverloaded  = 529
)

// systemOneState is the "state" of every request: the market snapshot
// and the RAG few-shot context. The questions refer to the two fields by
// name (`market`, `similar_past_cases`).
type systemOneState struct {
	Market           ScoutState  `json:"market"`
	SimilarPastCases rag.Context `json:"similar_past_cases"`
}

// Scout asks the FR-SCOUT-1 question group about req's state and returns
// the answers mapped onto ScoutResponse together with the total call
// latency (including retries). Retry policy: see evaluate.
func (c *Client) Scout(ctx context.Context, req ScoutRequest) (ScoutResponse, time.Duration, error) {
	result, latency, err := c.evaluate(ctx, "scout", liveCall, systemone.Request{
		State:     systemOneState{Market: req.State, SimilarPastCases: req.RAGContext},
		Model:     c.model,
		Questions: scoutQuestions,
	})
	if err != nil {
		return ScoutResponse{}, latency, err
	}
	return ScoutResponse{
		InterestingNow:   result.Noul(qInterestingNow),
		MomentumQuality:  result.Choice(qMomentumQuality).Value,
		LiquidityOk:      result.Noul(qLiquidityOk),
		AbnormalActivity: result.Noul(qAbnormalActivity),
		ModelID:          result.Model,
	}, latency, nil
}

// Trader asks the FR-TRADER-1 question group of req.QuestionVersion (empty:
// the build default, TraderQuestionVersion; an unregistered version is an
// error and nothing is sent) about req's state and returns the answers mapped
// onto TraderResponse together with the total call latency (including
// retries). Confidence is the confidence of the direction answer
// (FR-TRADER-2: Jev's self-reported certainty, not a verified probability).
// Retry policy: see evaluate. The call feeds the rolling error rate behind
// Healthy (the jev_api_down Kill Switch).
func (c *Client) Trader(ctx context.Context, req TraderRequest) (TraderResponse, time.Duration, error) {
	return c.trader(ctx, "trader", liveCall, req)
}

// ShadowTrader is Trader for the shadow (issue #872): the same call with a
// candidate version, but it never feeds the rolling error rate. A failing
// candidate must not trip the jev_api_down Kill Switch of the trading flow
// (the same reason Ask does not). It also never waits out a 429/529: the call
// fails at once with the *APIError so the shadow can stop (issue #883, see
// shadowCall), and the 429/529 it gets is not recorded in LastThrottle.
func (c *Client) ShadowTrader(ctx context.Context, req TraderRequest) (TraderResponse, time.Duration, error) {
	return c.trader(ctx, "trader-shadow", shadowCall, req)
}

func (c *Client) trader(ctx context.Context, label string, kind callKind, req TraderRequest) (TraderResponse, time.Duration, error) {
	questions, err := TraderQuestionsFor(req.QuestionVersion)
	if err != nil {
		return TraderResponse{}, 0, err
	}
	result, latency, err := c.evaluate(ctx, label, kind, systemone.Request{
		State:     systemOneState{Market: req.State, SimilarPastCases: req.RAGContext},
		Model:     c.model,
		Questions: questions,
	})
	if err != nil {
		return TraderResponse{}, latency, err
	}
	direction := result.Choice(qDirection)
	return TraderResponse{
		Direction:               direction.Value,
		Regime:                  result.Choice(qRegime).Value,
		EntryQuality:            result.Choice(qEntryQuality).Value,
		ToxicFlow:               result.Noul(qToxicFlow),
		LiquidityStressed:       result.Noul(qLiquidityStressed),
		ContinuationProbability: result.Noul(qContinuationProbability),
		Confidence:              direction.Confidence,
		ModelID:                 result.Model,
		InputTokens:             result.Usage.InputTokens,
		RequestBytes:            result.RequestBytes,
	}, latency, nil
}

// evaluate POSTs req to Endpoint and returns the validated result
// together with the total call latency. label names the call in the
// final error message and the structured JSON log line this emits for
// every call (non-functional.md §5.1 "Jev API latency / エラー率" -
// counting log lines by label doubles as "Scout呼び出し回数、Trader呼び出し回数"
// without a separate counter).
//
// Retry policy (overview.md §6): transport errors and 5xx are retried
// up to MaxAttempts; the first retry is immediate, later ones wait an
// exponentially growing backoff. 429 and 529 are retried too but back
// off from the first retry (hammering a rate-limited or overloaded API
// immediately makes it worse). 401, 422 and every other 4xx, and an
// invalid response body (systemone.ErrInvalidResponse), are never
// retried: repeating the same request cannot fix them. A call that
// ultimately fails returns the last error and the caller records no new
// jev_decisions entry (継続失敗でnew entry停止). Only a liveCall (Scout/Trader) also
// feeds c.errorRate; the call that first pushes its rolling error rate to
// c.errorRateThreshold notifies c.alerts (§5.2 "Jev APIエラー率上昇（しきい値超過）",
// errorrate.go). Ask (auxiliaryCall) and the shadow's calls do not (only
// kind == liveCall does): Luna/Sol/Opus questions are auxiliary and must not
// trip the jev_api_down Kill Switch of the trading flow.
//
// kind selects the call's standing (callKind). A 429/529 any call except a
// shadowCall receives is noted for LastThrottle; a shadowCall is never retried
// after one (issue #883).
func (c *Client) evaluate(ctx context.Context, label string, kind callKind, req systemone.Request) (result systemone.Result, latency time.Duration, err error) {
	trackHealth := kind == liveCall
	start := time.Now()
	defer func() {
		latency = time.Since(start)
		attrs := []any{"label", label, "duration_ms", latency.Milliseconds()}
		if err != nil {
			slog.Error("jev: api call failed", append(attrs, "error", err)...)
		} else {
			slog.Info("jev: api call completed", attrs...)
		}
		if !trackHealth {
			return
		}
		if rate, newlyBreached := c.errorRate.record(err != nil, c.errorRateThreshold); newlyBreached {
			if alertErr := c.alerts.JevAPIErrorRateExceeded(ctx, rate, c.errorRateThreshold); alertErr != nil {
				slog.Error("jev: error rate alert failed", "error", alertErr)
			}
		}
	}()

	body, err := json.Marshal(req)
	if err != nil {
		return result, 0, fmt.Errorf("jev: encode %s request: %w", label, err)
	}

	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		r, callErr := c.post(ctx, req, body)
		if callErr == nil {
			r.RequestBytes = len(body)
			return r, 0, nil
		}
		lastErr = callErr
		if kind != shadowCall {
			c.noteThrottle(callErr)
		}

		if !retryable(callErr) || (kind == shadowCall && throttleStatus(callErr) != 0) {
			err = fmt.Errorf("jev: %s call failed: %w", label, callErr)
			return result, 0, err
		}
		if attempt == c.maxAttempts {
			break
		}
		if delay, ok := c.retryDelay(callErr, attempt); ok {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				err = ctx.Err()
				return result, 0, err
			case <-timer.C:
			}
		}
	}
	err = fmt.Errorf("jev: %s call failed after %d attempts: %w", label, c.maxAttempts, lastErr)
	return result, 0, err
}

// throttleStatus returns 429 or 529 when err is the API answering one of them,
// else 0.
func throttleStatus(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == statusRateLimited || apiErr.StatusCode == statusOverloaded) {
		return apiErr.StatusCode
	}
	return 0
}

// retryable reports whether err may succeed on a repeated request.
func retryable(err error) bool {
	if errors.Is(err, systemone.ErrInvalidResponse) || errors.Is(err, httpbody.ErrTooLarge) {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500 || apiErr.StatusCode == statusRateLimited
	}
	return true // transport error
}

// retryDelay returns how long to wait before the attempt that follows
// failed attempt number attempt (1-based), and false if it should be
// made immediately.
func (c *Client) retryDelay(err error, attempt int) (time.Duration, bool) {
	n := attempt - 2 // first retry immediate, backoff from the second
	var apiErr *APIError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == statusRateLimited || apiErr.StatusCode == statusOverloaded) {
		n = attempt - 1 // throttled: back off from the first retry
	}
	if n < 0 {
		return 0, false
	}
	return c.retryBaseDelay * time.Duration(1<<uint(n)), true
}

// post makes one HTTP attempt and decodes/validates the answer set.
func (c *Client) post(ctx context.Context, req systemone.Request, body []byte) (systemone.Result, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+Endpoint, bytes.NewReader(body))
	if err != nil {
		return systemone.Result{}, fmt.Errorf("jev: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return systemone.Result{}, fmt.Errorf("jev: request: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, err := httpbody.ReadAll(httpResp.Body, httpbody.DefaultMaxBytes)
	if err != nil {
		return systemone.Result{}, fmt.Errorf("jev: read response body: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return systemone.Result{}, &APIError{StatusCode: httpResp.StatusCode, Body: string(respBody)}
	}
	return systemone.Decode(req, respBody)
}
