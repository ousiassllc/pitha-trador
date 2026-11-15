//go:build race

package exporttests

// raceEnabled is true when the tests run under the race detector, which slows the SQLite driver about
// 25 times: the full-size run is then left to the normal test run (the limit and the numbers do not
// depend on concurrency).
const raceEnabled = true
