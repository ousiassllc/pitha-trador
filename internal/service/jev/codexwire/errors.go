package codexwire

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Reasons of a CallError: fixed words that say how the call failed. None carries
// text of the CLI's output.
const (
	ReasonStart     = "could not start"
	ReasonTurn      = "turn.failed"
	ReasonExit      = "non-zero exit"
	ReasonNoTurn    = "no turn.completed"
	ReasonCancelled = "stopped"
)

// CallError is a `codex exec` call that failed: the process did not start, it
// exited non-zero, it reported turn.failed, it ended without turn.completed, or
// its context ended (the cause is then context.DeadlineExceeded or
// context.Canceled, see Unwrap). The caller stops the provider on it and never
// retries.
type CallError struct {
	Reason string
	// UsageLimit is true when the failure's text matched a usage-limit message
	// (see MatchUsageLimit). It is a label for a recognised text, not a measurement:
	// a failure that did not match is still a failure.
	UsageLimit bool
	// Until is the time the text said the limit resets, zero when it gave none or
	// when it was not usable (see ParseResetTime).
	Until time.Time
	cause error
}

func (e *CallError) Error() string {
	if e.UsageLimit {
		return "codexwire: codex exec failed (" + e.Reason + "; usage limit)"
	}
	return "codexwire: codex exec failed (" + e.Reason + ")"
}

func (e *CallError) Unwrap() error { return e.cause }

// usageLimit matches the usage-limit messages of the Codex CLI known from its
// public source (they can change with the CLI's version, which is why a match
// only labels a failure).
var usageLimit = regexp.MustCompile(`(?i)(hit your usage limit|usage limit for|out of credits|hit your spend cap|quota exceeded)`)

// MatchUsageLimit reports whether text is a usage-limit message of the CLI.
func MatchUsageLimit(text string) bool { return usageLimit.MatchString(text) }

// resetAt picks the "try again at <time>" part of a usage-limit message.
var resetAt = regexp.MustCompile(`(?i)try again (?:at|in)\s+([^.\n]+)`)

// maxResetAhead bounds how far a reset time may lie ahead to be believed.
const maxResetAhead = 8 * 24 * time.Hour

// resetLayouts are the time formats tried for the text after "try again at". The
// real wording is not verified (nothing was run against a login): a text that
// does not parse gives no time, and the caller stops until the end of the JST day.
var resetLayouts = []string{
	time.RFC3339,
	"Jan 2, 2006 3:04 PM",
	"Jan 2, 2006 3:04PM",
	"Jan 2, 2006 15:04",
	"Jan 2 2006 3:04 PM",
	"2006-01-02 15:04",
	// Without a year: the year of now is taken.
	"Jan 2, 3:04 PM",
	"Jan 2, 3:04PM",
	"Jan 2, 15:04",
}

var ordinalSuffix = regexp.MustCompile(`(\d)(st|nd|rd|th)\b`)

// ParseResetTime reads the reset time out of a usage-limit message, in loc (the
// message gives none). It returns false for a message without a time, a time that
// does not parse, one that is not after now and one further than eight days ahead.
func ParseResetTime(text string, now time.Time, loc *time.Location) (time.Time, bool) {
	m := resetAt.FindStringSubmatch(text)
	if m == nil {
		return time.Time{}, false
	}
	s := ordinalSuffix.ReplaceAllString(strings.TrimSpace(m[1]), "$1")
	for _, layout := range resetLayouts {
		t, err := time.ParseInLocation(layout, s, loc)
		if err == nil && t.Year() == 0 {
			t = t.AddDate(now.In(loc).Year(), 0, 0)
		}
		if err == nil && t.After(now) && t.Before(now.Add(maxResetAhead)) {
			return t, true
		}
	}
	return time.Time{}, false
}

func startError(err error) *CallError {
	return &CallError{Reason: ReasonStart, cause: fmt.Errorf("start codex: %w", err)}
}
