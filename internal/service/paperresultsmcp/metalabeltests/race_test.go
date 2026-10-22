//go:build race

package metalabeltests

import "time"

// The race detector slows the in-process feature rebuild and the SQLite driver
// several-fold, so the perf tests take their budgets from here (see norace_test.go).

// serverTimeout raises the MCP's own read timeout so that a slow runner fails
// the perf tests on their budget (with the measured time), not on a 504.
const serverTimeout = time.Minute

// readBudgets are the finite wall-clock budgets of the perf tests under -race
// (CI runs only -race, so the 3 s of the normal build is not judged there).
// Measured here with -race (32 cores, unloaded / with the whole
// paperresultsmcp and metalabel tree running in parallel): 30 days 1.1 s / 2.1 s,
// 90 days 3.1 s / 5.0-5.5 s, 366-day jev_models 1.45 s / 1.45 s; without -race
// 0.11-0.14 s, 0.29-0.31 s and 0.045 s. A CI runner measured over 2 times slower
// than that on the 366-day call (it passed the 3 s budget here and failed it
// there). The budgets are 5 to 10 times the measurements: a slower runner passes,
// an order-of-magnitude regression (a lost index, a per-row query) does not.
var readBudgets = struct{ days30, days90, days366 time.Duration }{
	days30:  10 * time.Second,
	days90:  30 * time.Second,
	days366: 15 * time.Second,
}
