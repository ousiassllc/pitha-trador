// Package jev is the Jev adapter (docs/architecture/overview.md §6): an
// HTTP client for the Jev API (client.go), the Scout/Trader request and
// response schemas (schemas.go), prompt/question-set version tracking
// (prompt_version.go), and Jev Scout's FR-SCOUT-1〜3 evaluation and
// persistence logic (scout.go).
//
// The Jev API key is held only in this package's in-process Config, never
// written to disk (overview.md §6, mirroring
// internal/service/marketdata's kabuステーションAPI token handling). A
// failed Jev call is retried once immediately, then with exponential
// backoff, per client.go's retry policy; once retries are exhausted the
// caller records no new jev_decisions entry (overview.md §6 "継続失敗で
// new entry停止").
//
// This sub-scope introduces Jev Scout (scout.go). Jev Trader
// (functional.md §4.5, trader.go) is a later sub-scope built on the same
// client/schemas/prompt_version foundation.
package jev
