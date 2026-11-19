package jevtrialsettings

import (
	"errors"
	"time"
)

// LLMCutoffLayout is the date format of Arm.LLMCutoff.
const LLMCutoffLayout = time.DateOnly

// validateCutoff checks Arm.LLMCutoff: empty (not published) or a real
// YYYY-MM-DD date. No date is ever filled in for the operator.
func (a Arm) validateCutoff() error {
	if a.LLMCutoff == "" {
		return nil
	}
	if _, err := time.Parse(LLMCutoffLayout, a.LLMCutoff); err != nil {
		return errors.New("must be empty or a YYYY-MM-DD date")
	}
	return nil
}

// TargetableFrom is the first instant a decision may be replayed against the
// arm's model: the start (UTC) of the day AFTER the published training cutoff,
// so only decisions dated after the cutoff are targeted. known is false while
// the cutoff is not published (empty LLMCutoff): the replay then still runs
// but its rows are marked unverified and reference-only.
func (a Arm) TargetableFrom() (from time.Time, known bool) {
	day, err := time.Parse(LLMCutoffLayout, a.LLMCutoff)
	if err != nil {
		return time.Time{}, false
	}
	return day.AddDate(0, 0, 1), true
}
