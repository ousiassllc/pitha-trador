// Package codexwire calls one trial arm's model through the official Codex CLI
// logged in with a ChatGPT subscription (issue #1081): `codex exec --json
// --output-schema`, started as a child process with a dedicated CODEX_HOME. It is
// the Codex counterpart of trialwire: Client.Trader answers the same FR-TRADER-1
// question group and returns a trialwire.Response, so the trial runner does not
// change.
//
// What the package guarantees:
//   - The login is the person's: pitha never reads, copies or logs the Codex
//     credentials (the auth.json under CODEX_HOME is not opened), holds no token,
//     and sends the CLI no API key. The child's environment is an allow-list
//     (childEnv), so CODEX_API_KEY, OPENAI_API_KEY and CODEX_ACCESS_TOKEN can never
//     reach it, whatever the parent's environment holds.
//   - A call is judged by the CLI's exit code and its JSONL turn events only: it
//     succeeded when the process exited 0 and a turn.completed was seen and no
//     turn.failed was. Anything else is a *CallError. The text of a failure is
//     matched against known usage-limit messages only to LABEL the failure
//     (CallError.UsageLimit); it is never stored, logged or shown.
//   - A child is started in its own process group and the whole group is killed
//     when the call's context ends.
//   - The answer is validated as strictly as the OpenRouter path (it is turned
//     into a System One answer set and read with systemone.DecodeTrial).
//
// A Codex arm is a trial arm only; nothing here is reachable from the live Jev.
package codexwire
