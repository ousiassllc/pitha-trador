// Package origin tells how a Paper trade was entered (issue #829): the Policy
// Engine's meta-labeling rule, the Paper-only bootstrap rule, or an order
// proposal. It is decided by trade_signals.policy_version of the signal
// behind the entry order (positions.entry_order_id ->
// paper_orders.trade_signal_id), so an exit never changes it: a position the
// Policy Engine opened and an order proposal closed is still a meta_model
// trade.
package origin

import (
	"database/sql"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
)

// Values of a trade's origin.
const (
	// MetaModel: the meta-labeling entry rule: any policy_version that is
	// neither of the two below ("policy-v2" and its "+<applied proposal>"
	// variants).
	MetaModel = "meta_model"
	// Bootstrap: the Paper-only bootstrap entry (issue #798),
	// domain.PolicyVersionPaperBootstrap with an optional "+<applied proposal>".
	Bootstrap = "bootstrap"
	// OrderProposal: an order proposal (issue #828), orderproposal.PolicyVersion.
	OrderProposal = "order_proposal"
	// Unknown: the entry order has no trade signal (or no order row), so
	// nothing says where it came from.
	Unknown = "unknown"
)

// All lists every origin in the order the tools report them.
func All() []string { return []string{MetaModel, Bootstrap, OrderProposal, Unknown} }

// Of maps the policy_version of a trade's entry signal (invalid when the
// position has no signal) to its origin.
func Of(policyVersion sql.NullString) string {
	v := policyVersion.String
	switch {
	case !policyVersion.Valid:
		return Unknown
	case v == orderproposal.PolicyVersion:
		return OrderProposal
	case v == domain.PolicyVersionPaperBootstrap || strings.HasPrefix(v, domain.PolicyVersionPaperBootstrap+"+"):
		return Bootstrap
	}
	return MetaModel
}
