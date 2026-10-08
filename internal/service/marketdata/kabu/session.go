package kabu

import (
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// SessionStatusOf maps the kabu token issuance outcome onto the neutral
// broker.SessionStatus. The kabu failure causes keep their names
// (unreachable / not_logged_in / api_disabled / bad_password / unknown /
// rejected) and the operator guidance is kabu's own text
// (marketdata.TokenStatus.Guidance).
func SessionStatusOf(ts marketdata.TokenStatus) broker.SessionStatus {
	return broker.SessionStatus{
		Issue:    sessionIssue(ts.Issue),
		Code:     ts.Code,
		Guidance: ts.Guidance(),
		Failures: ts.Failures,
		Since:    ts.Since,
	}
}

func sessionIssue(issue marketdata.TokenIssue) broker.SessionIssue {
	switch issue {
	case marketdata.TokenIssueNone:
		return broker.SessionIssueNone
	case marketdata.TokenIssueUnreachable:
		return broker.SessionIssueUnreachable
	case marketdata.TokenIssueNotLoggedIn:
		return broker.SessionIssueNotLoggedIn
	case marketdata.TokenIssueAPIDisabled:
		return broker.SessionIssueAPIDisabled
	case marketdata.TokenIssueBadPassword:
		return broker.SessionIssueBadPassword
	case marketdata.TokenIssueRejected:
		return broker.SessionIssueRejected
	default:
		return broker.SessionIssueUnknown
	}
}
