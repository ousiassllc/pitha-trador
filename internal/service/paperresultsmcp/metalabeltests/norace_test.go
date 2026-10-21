//go:build !race

package metalabeltests

// raceEnabled is false in a normal build (see race_test.go).
const raceEnabled = false
