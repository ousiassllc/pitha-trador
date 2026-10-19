package opsettings

import (
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/paperbootstrapsettings"
)

// WithTradingMode records the trading mode this Service runs in. The Paper-only
// bootstrap entry switch (issue #798) can be turned on only in
// metalabelsettings.ModePaper; any other mode - Live, or a Service built
// without this call - refuses to store it as on (turning it off stays
// allowed), and shows an already stored "on" as inactive. This is the Settings
// side of the hard rule; the Policy Engine independently ignores the switch
// outside Paper (policy.WithPaperBootstrap).
func (s *Service) WithTradingMode(mode metalabelsettings.Mode) *Service {
	s.mode = mode
	return s
}

// paperBootstrapRefusal is the reason Save gives for turning the bootstrap on
// outside Paper.
const paperBootstrapRefusal = "Paper 以外（Live）では有効にできません。ブートストラップは Paper 限定です"

// paperBootstrapInactiveWarning is the hint on a bootstrap row whose stored
// value is on while the mode is not Paper: the value is ignored.
const paperBootstrapInactiveWarning = "Live では無効です。保存済みの値は無視され、ブートストラップは働きません。"

// validatePaperBootstrap checks one value of the bootstrap switch.
func (s *Service) validatePaperBootstrap(encoded string) error {
	enabled, err := paperbootstrapsettings.Apply(encoded)
	if err != nil {
		return &InvalidValueError{Reason: err.Error()}
	}
	if enabled && s.mode != metalabelsettings.ModePaper {
		return &InvalidValueError{Reason: paperBootstrapRefusal}
	}
	return nil
}

// paperBootstrapWarning is the warning on the bootstrap row for its current
// text.
func (s *Service) paperBootstrapWarning(current string) string {
	if current == paperbootstrapsettings.Text(true) && s.mode != metalabelsettings.ModePaper {
		return paperBootstrapInactiveWarning
	}
	return ""
}
