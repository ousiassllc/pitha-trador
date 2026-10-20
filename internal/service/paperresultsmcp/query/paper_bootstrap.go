package query

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Codes of PaperBootstrap.Code (issue #798): fixed, never free text.
// health_today (#808) reads the same state through PaperBootstrapUse.
const (
	// PaperBootstrapInUse: at least one signal in the range was decided on the
	// Paper-only bootstrap entry (the legacy threshold rule, used while no
	// meta-model is adopted).
	PaperBootstrapInUse = "in_use"
	// PaperBootstrapNotInUse: none was; the bootstrap is off, has nothing to
	// do (a model is adopted, or no candidate arrived), or Paper is not running.
	PaperBootstrapNotInUse = "not_in_use"
)

// PaperBootstrap is the Paper-only bootstrap entry as observed in the signals
// of a range. The switch itself lives in runtime_settings, which this MCP
// never reads (it can hold secret paths), so what is reported is whether it
// took effect: every signal the bootstrap decided carries
// domain.PolicyVersionPaperBootstrap in trade_signals.policy_version.
type PaperBootstrap struct {
	Code string `json:"code"`
	// Signals is how many signals in the range were decided on the bootstrap
	// entry (LONG, SHORT and NONE alike).
	Signals int `json:"signals"`
	// LastSignalAt is the newest such signal (UTC RFC3339), nil when none.
	LastSignalAt *string `json:"last_signal_at"`
}

// PaperBootstrapUse reports whether signals decided on the bootstrap entry
// exist in r.
func PaperBootstrapUse(ctx context.Context, q Q, r Range) (PaperBootstrap, error) {
	from, to := r.bounds()
	var (
		out  PaperBootstrap
		last sql.NullString
	)
	err := q.QueryRowContext(ctx, `
SELECT COUNT(*), MAX(timestamp)
FROM trade_signals
WHERE timestamp >= ? AND timestamp < ? AND (policy_version = ? OR policy_version GLOB ?)`,
		from, to, domain.PolicyVersionPaperBootstrap, domain.PolicyVersionPaperBootstrap+"+*").Scan(&out.Signals, &last)
	if err != nil {
		return PaperBootstrap{}, fmt.Errorf("query: paper bootstrap use: %w", err)
	}
	out.LastSignalAt = formatOutPtr(last)
	out.Code = PaperBootstrapNotInUse
	if out.Signals > 0 {
		out.Code = PaperBootstrapInUse
	}
	return out, nil
}
