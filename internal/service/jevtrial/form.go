package jevtrial

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
)

// Form is what the Settings form submits. Cap is nil for a blank field (the
// default applies); so are CodexCallCap and CodexTokenCap.
type Form struct {
	Enabled bool
	Arms    []jevtrialsettings.Arm
	Cap     *float64
	// CodexEnabled is the switch of the ChatGPT subscription provider (off by
	// default); CodexCallCap and CodexTokenCap are its daily caps.
	CodexEnabled                bool
	CodexCallCap, CodexTokenCap *int
}

// Save validates the whole form first and only then writes it, so a rejected
// form changes nothing.
func (s *Service) Save(ctx context.Context, f Form) error {
	armsValue, err := ValidateArms(f.Arms)
	if err != nil {
		return err
	}
	if f.Cap != nil {
		if err := ValidateCap(*f.Cap); err != nil {
			return err
		}
	}
	if err := ValidateCodex(f, f.Arms); err != nil {
		return err
	}
	// Turning the trial off comes first, so a later write that fails cannot leave
	// it on; turning it on comes last, after everything it depends on is saved.
	// The Codex switch follows the same order.
	if !f.Enabled {
		if err := s.SetEnabled(ctx, false); err != nil {
			return err
		}
	}
	if !f.CodexEnabled {
		if err := s.setCodexEnabled(ctx, false); err != nil {
			return err
		}
	}
	if err := s.settings.Set(ctx, jevtrialsettings.KeyArms, armsValue, s.now()); err != nil {
		return err
	}
	if err := s.setCodexCap(ctx, jevtrialsettings.KeyCodexDailyCallCap, f.CodexCallCap); err != nil {
		return err
	}
	if err := s.setCodexCap(ctx, jevtrialsettings.KeyCodexDailyTokenCap, f.CodexTokenCap); err != nil {
		return err
	}
	if f.Cap == nil {
		err = s.ResetCap(ctx)
	} else {
		err = s.SetCap(ctx, *f.Cap)
	}
	if err != nil {
		return err
	}
	if f.CodexEnabled {
		if err := s.setCodexEnabled(ctx, true); err != nil {
			return err
		}
	}
	if f.Enabled {
		return s.SetEnabled(ctx, true)
	}
	return nil
}
