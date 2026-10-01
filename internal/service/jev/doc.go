// Package jev is the Jev adapter (docs/architecture/overview.md §6): a
// client for the TypeSafe AI evaluation API (POST {BaseURL}/v1/systemone,
// https://docs.typesafe.ai/api) that asks typed questions about a market
// state and maps the answers onto the domain-level ScoutResponse /
// TraderResponse (client.go, evaluate.go), the Scout/Trader question
// definitions (questions.go, questions_trader.go), the domain-level
// request/response types and market state (schemas.go), prompt/question-
// set version tracking (prompt_version.go), and Jev Scout's FR-SCOUT-1〜3
// and Jev Trader's FR-TRADER-1〜3 evaluation and persistence logic
// (scout.go, trader.go). The wire format and strict response validation
// live in the systemone sub-package; jevtest fakes the API for tests.
//
// The Jev API key is held only in this package's in-process Config, never
// written to disk (overview.md §6, mirroring
// internal/service/marketdata's kabuステーションAPI token handling). A
// failed Jev call is retried per evaluate.go's retry policy (transport
// errors and 5xx: first retry immediate, then exponential backoff;
// 429/529: backoff from the first retry; 401/422 and invalid responses:
// never); once the call fails the caller records no new jev_decisions
// entry (overview.md §6 "継続失敗でnew entry停止").
package jev
