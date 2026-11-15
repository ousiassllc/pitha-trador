// Package codexflow holds the flow tests of the trial's ChatGPT subscription
// provider (issue #1081, internal/service/jev/trial/codex.go and codexwire): the
// runner with fake Codex arms, and the application's wiring against a mock CLI (a
// shell script). No real Codex CLI, login, network or key is involved. It sits
// next to trialflow (whose harness it does not share, to keep that directory
// within its line limit).
package codexflow
