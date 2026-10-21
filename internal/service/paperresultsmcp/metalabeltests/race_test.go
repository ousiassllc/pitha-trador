//go:build race

package metalabeltests

// raceEnabled is true under the race detector, which slows the in-process
// feature rebuild about tenfold; the read-budget test then only logs times.
const raceEnabled = true
