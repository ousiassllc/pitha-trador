package session

import "github.com/ousiassllc/pitha-trador/internal/service/broker"

// Classify exposes the login failure classification.
func Classify(err error) (issue broker.SessionIssue, code int, guidance string) {
	f := classify(err)
	return f.issue, f.code, f.guidance
}

// Guidance texts the tests compare against.
const (
	GuidanceClock     = guidanceClock
	GuidanceDocuments = guidanceDocuments
)

// Tuning the tests step the clock by.
const (
	MinReloginInterval = minReloginInterval
	ContentionWindow   = contentionWindow
)
