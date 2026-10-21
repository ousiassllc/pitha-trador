//go:build race

package metalabeltests

// raceEnabled is true under the race detector, which slows the in-process
// feature rebuild about tenfold: the 90-day readiness call is then only logged,
// and the 366-day jev_models wall-clock test is not built (norace_test.go).
const raceEnabled = true
