// Package decideflow holds the tests of policy.Engine.Decide's reject reasons
// that need no database (the reason's width and the untradable-bar and
// lendability conditions); it has no non-test code. They live in their own
// directory to keep internal/service/policy within the repository's directory
// size limit (as metaflow does).
package decideflow
