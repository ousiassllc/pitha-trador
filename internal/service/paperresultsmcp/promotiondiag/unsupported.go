package promotiondiag

import (
	"errors"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// The fixed reasons of an UnsupportedError. They name the rule and never quote what the caller sent,
// so the MCP can show one as error.data.detail.
const (
	// ReasonSettingsSetAsCurrent: an experiment / setting is only ever a candidate.
	ReasonSettingsSetAsCurrent = "current に experiment / setting は指定できません。experiment / setting は candidate にだけ指定できます"
	// ReasonCohortAsCurrent: a live_cohort judged other decisions, so no row can be paired with it.
	ReasonCohortAsCurrent = "live_cohort は別の判断を見ているので current にできません"
	// ReasonCurrentFixed: an experiment / setting candidate is compared with its own before period.
	ReasonCurrentFixed = "experiment / setting を candidate にするときの current は、その実験の前の期間で決まっています。current は指定できません"
	// ReasonSettingNotListed: only the settings on the MCP's whitelist can have an experiment (its confounders can be checked).
	ReasonSettingNotListed = "setting の id は、MCP の設定ホワイトリストにあるキー（調整の実験にできるキー）だけです"
)

// UnsupportedError is a kind or a combination of kinds the checklist cannot check. The MCP answers it
// as unsupported_candidate_kind (422), apart from invalid_argument (which it also unwraps to, for a
// caller that only knows query.ErrBadCursor).
type UnsupportedError struct{ Reason string }

func (e *UnsupportedError) Error() string {
	return "promotiondiag: unsupported_candidate_kind: " + e.Reason
}

// UnsupportedKind is the fixed reason the MCP shows as error.data.detail (it names the rule, never the caller's value).
func (e *UnsupportedError) UnsupportedKind() string { return e.Reason }

// Unwrap makes it a bad argument for any classifier that does not know it.
func (e *UnsupportedError) Unwrap() error { return query.ErrBadCursor }

func unsupported(reason string) error { return &UnsupportedError{Reason: reason} }

// IsUnsupported reports whether err is an *UnsupportedError and returns its fixed reason.
func IsUnsupported(err error) (reason string, ok bool) {
	var ue *UnsupportedError
	if errors.As(err, &ue) {
		return ue.Reason, true
	}
	return "", false
}
