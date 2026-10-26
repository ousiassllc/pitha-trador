package jev

import "errors"

// ErrPersistDecision matches (errors.Is) the error of a Trader call that got
// Jev's answer but could not record it in jev_decisions, so the Policy Engine
// can tell it from a failed API call (issue #912). The message of the error is
// unchanged.
var ErrPersistDecision = errors.New("jev: trader decision not persisted")

// persistError is err, matching ErrPersistDecision.
type persistError struct{ error }

func (e persistError) Unwrap() error { return e.error }

func (persistError) Is(target error) bool { return target == ErrPersistDecision }
