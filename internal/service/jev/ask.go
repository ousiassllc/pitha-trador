package jev

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

// Configured reports whether an API key is set. Scout/Trader keep working
// (and fail per call) without one; the auxiliary Luna/Sol/Opus roles that
// default to Jev (internal/service/assist, issue #273) use it to disable
// themselves instead.
func (c *Client) Configured() bool {
	return c != nil && c.apiKey != ""
}

// Ask POSTs a caller-defined question set about state to Endpoint with the
// client's model, retry policy and validation (see evaluate), and returns
// the validated answers. It is how Luna (news classification), Sol
// (candidate selection) and Opus (proposal review) use Jev (issue #273).
//
// Unlike Scout/Trader, Ask calls do not feed the rolling error rate behind
// Healthy and the Slack alert: these roles are auxiliary, so a failing
// news or self-improvement question must never trip the jev_api_down Kill
// Switch of the trading flow. label names the call in the log line.
func (c *Client) Ask(ctx context.Context, label string, state any, questions map[string]systemone.Question) (systemone.Result, error) {
	result, _, err := c.evaluate(ctx, label, false, systemone.Request{
		State:     state,
		Model:     c.model,
		Questions: questions,
	})
	return result, err
}
