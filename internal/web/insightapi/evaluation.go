package insightapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/pitha-trador/internal/service/actuals"
)

// jst bounds the `from`/`to` days: the Tokyo Stock Exchange's time zone.
// Japan has no DST, so a fixed zone is exact and needs no tzdata.
var jst = time.FixedZone("JST", 9*60*60)

const dateLayout = "2006-01-02"

// EvaluationInput is the query of `GET /api/v1/performance/evaluation`.
type EvaluationInput struct {
	From string `query:"from" doc:"First JST day (YYYY-MM-DD) of the closed-at range; omitted: from the beginning."`
	To   string `query:"to" doc:"Last JST day (YYYY-MM-DD, inclusive) of the closed-at range; omitted: up to now."`
}

// EvaluationOutput is the response of `GET /api/v1/performance/evaluation`
// (docs/api/endpoints/huma-api-insights.md).
type EvaluationOutput struct {
	Body actuals.Evaluation
}

// Evaluation implements `GET /api/v1/performance/evaluation` (issue #777):
// the read-only realized-performance evaluation over [from 00:00 JST, to+1
// 00:00 JST), taken from the provider when it implements actuals.Evaluator
// (an empty evaluation otherwise). It writes nothing and returns no market
// data.
func (h *Handler) Evaluation(ctx context.Context, in *EvaluationInput) (*EvaluationOutput, error) {
	var r actuals.Range
	if in.From != "" {
		from, err := time.ParseInLocation(dateLayout, in.From, jst)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("from must be a YYYY-MM-DD date")
		}
		r.From = from
	}
	if in.To != "" {
		to, err := time.ParseInLocation(dateLayout, in.To, jst)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("to must be a YYYY-MM-DD date")
		}
		r.To = to.AddDate(0, 0, 1)
	}
	if !r.From.IsZero() && !r.To.IsZero() && !r.To.After(r.From) {
		return nil, huma.Error422UnprocessableEntity("to must not be before from")
	}
	e, err := actuals.EvaluatorOf(h.provider).Evaluate(ctx, r)
	if err != nil {
		slog.ErrorContext(ctx, "insightapi: performance evaluation", "error", err)
		return nil, huma.Error500InternalServerError("evaluate performance failed", err)
	}
	return &EvaluationOutput{Body: e}, nil
}
