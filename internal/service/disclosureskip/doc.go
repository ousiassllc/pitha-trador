// Package disclosureskip records, without any effect on trading, whether a
// mechanical "skip the entry after an earnings disclosure" rule would have
// applied to each Jev entry-candidate decision (issue #935, FR-DISCLOSURE-SKIP-1〜5;
// docs/requirements/functional/components-platform.md §4.16).
//
// The rule (rule.go, kind.go) is a pure function: a decision at time t on a
// symbol is a "skip" when the symbol disclosed an earnings report (決算短信)
// or an earnings forecast revision (業績予想の修正) at or before t, on t's JST
// day or at/after 15:00 of the preceding TSE trading day. A disclosure after t
// is never looked at. When the feed did not cover that window (a failed,
// missing or truncated fetch) and nothing matched, the answer is "unknown",
// never "no skip".
//
// The inputs come from the feed News Ingest already polls: RecordingFeed
// (observe.go) wraps its newsfeed.Feed, returns the inner result untouched and
// hands a copy of each fetch to a background writer through a bounded queue
// (store.go); Evaluator (evaluator.go) later reads the stored disclosures and
// fetches and the jev_decisions rows (read only) and stores one verdict per
// decision. No request is added to the feed, nothing is written from the
// trading path, and a failure here is only logged: News Ingest's results,
// Policy, Risk and Execution never see this package.
package disclosureskip
