// Package paperbootstrap is the decision-point side of the Paper-only bootstrap
// entry (issue #798): while no meta-labeling model is adopted every candidate
// is skipped before the Risk Engine, so Paper opens no positions and the labels
// the first model needs never accumulate. With the Settings switch on - and
// only in Paper - the Policy Engine enters on the legacy threshold rule instead
// until a model is adopted (internal/service/policy.WithPaperBootstrap).
//
// The hard rule lives in Entry.Active: outside Paper the switch is not even
// read, so it has no effect however it is set.
package paperbootstrap

import (
	"context"
	"log/slog"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Version is trade_signals.policy_version of a bootstrap decision, in place of
// the meta-labeling rule's version. It is short so that it plus a proposal's
// "+sol-123456" suffix still fits the column's VARCHAR(20).
const Version = domain.PolicyVersionPaperBootstrap

// Source reports whether the Settings switch is on; it is read at every
// decision, so a change applies without a restart.
// paperbootstrapsettings.Source implements it.
type Source interface {
	PaperBootstrapEnabled(ctx context.Context) (bool, error)
}

// Entry is the bootstrap wiring: the trading mode the engine decides for and
// the switch.
type Entry struct {
	mode metalabelsettings.Mode
	src  Source
}

// New returns an Entry for mode, which must be metalabelsettings.ModePaper for
// the bootstrap to ever apply: any other mode, Live or unset, never does.
func New(mode metalabelsettings.Mode, src Source) *Entry { return &Entry{mode: mode, src: src} }

// Active reports whether the bootstrap applies to this decision: Paper, with
// the switch on. The mode is checked before the switch is read. An unreadable
// switch is off: the model rule then skips the entry as before.
func (e *Entry) Active(ctx context.Context) bool {
	if e.mode != metalabelsettings.ModePaper {
		return false
	}
	on, err := e.src.PaperBootstrapEnabled(ctx)
	if err != nil {
		slog.Error("policy: read the Paper bootstrap switch; keeping the meta-label entry rule", "error", err)
		return false
	}
	return on
}

// PolicyVersion is version, the meta-labeling rule's policy_version (metaVersion
// with an optional "+<applied proposal>" suffix), re-labelled as a bootstrap
// decision: the suffix is kept.
func PolicyVersion(version, metaVersion string) string {
	return Version + strings.TrimPrefix(version, metaVersion)
}

// LogDecided marks a decision as a bootstrap entry in the log, one line per
// decision (LONG/SHORT and NONE alike).
func LogDecided(instrumentID int64, symbol, direction, rejectReason, version string) {
	slog.Info("policy: paper bootstrap entry decided on the legacy rule (no active meta-model)",
		"instrument_id", instrumentID, "symbol", symbol, "direction", direction,
		"policy_version", version, "reject_reason", rejectReason)
}
