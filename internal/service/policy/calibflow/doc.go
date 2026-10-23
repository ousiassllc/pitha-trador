// Package calibflow holds the end-to-end test of the Policy Handler's confidence
// bucket check over the live Trader prompt version's samples (issue #881,
// calibrationgate); it has no non-test code. It lives in its own directory to
// keep internal/service/policy within the repository's directory size limit (as
// metaflow does).
package calibflow
