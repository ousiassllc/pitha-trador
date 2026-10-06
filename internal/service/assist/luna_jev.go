package assist

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

const (
	lunaQSentiment = "sentiment"
	lunaQEventType = "event_type"

	// lunaNoHeadline stands in for the summary of an item without headline.
	lunaNoHeadline = "(見出しなし)"
)

// lunaGuide is appended to both questions so each is self-contained (the
// API evaluates questions independently).
const lunaGuide = "Context: `news` is one news article or timely disclosure about a stock listed on the Tokyo Stock Exchange. " +
	"The `body` may be only a link to the original document; judge on the headline in that case."

// lunaQuestions are Luna's two FR-LUNA-2 questions, answered by Jev as
// choice questions (Jev returns no free text). Changing the wording or the
// options changes what the recorded sentiment/event type mean.
var lunaQuestions = map[string]systemone.Question{
	lunaQSentiment: systemone.ChoiceQuestion(
		"What is the likely short-term effect of this news on the stock price? "+lunaGuide,
		systemone.Option{Name: domain.NewsSentimentBullish, Rubric: "Clearly positive for the price: upward revision, higher dividend, buyback, favourable deal or approval."},
		systemone.Option{Name: domain.NewsSentimentBearish, Rubric: "Clearly negative for the price: downward revision, dividend cut, offering, penalty, or loss."},
		systemone.Option{Name: domain.NewsSentimentNeutral, Rubric: "No clear price effect: routine notice, administrative change, or direction cannot be told from the headline."},
	),
	lunaQEventType: systemone.ChoiceQuestion(
		"Which kind of event does this news report? "+lunaGuide,
		systemone.Option{Name: domain.NewsEventEarnings, Rubric: "Financial results announcement (決算短信, quarterly or full-year results, results summary)."},
		systemone.Option{Name: domain.NewsEventGuidance, Rubric: "Revision of earnings or dividend forecasts, or new guidance (業績予想・配当予想の修正)."},
		systemone.Option{Name: domain.NewsEventMA, Rubric: "M&A, tender offer, capital or business alliance, stock exchange, merger, split, delisting-related restructuring."},
		systemone.Option{Name: domain.NewsEventRegulation, Rubric: "Regulatory, legal or exchange action: administrative order, investigation, designation, caution, penalty."},
		systemone.Option{Name: domain.NewsEventOther, Rubric: "Anything else, including routine notices such as share buyback status or officer changes."},
	),
}

// lunaState is the state Jev evaluates for one news item.
type lunaState struct {
	News LunaRequest `json:"news"`
}

// classifyWithJev classifies item with two Jev choice questions. Jev has no
// free-text output, so the summary is the headline (code side).
func (l *Luna) classifyWithJev(ctx context.Context, item NewsItem) (Classification, error) {
	state := lunaState{News: LunaRequest{Symbol: item.Symbol, Headline: item.Headline, Body: item.Body, PublishedAt: item.PublishedAt}}
	result, err := l.jev.Ask(ctx, "luna", state, lunaQuestions)
	if err != nil {
		return Classification{}, err
	}
	summary := item.Headline
	if summary == "" {
		summary = lunaNoHeadline
	}
	return Classification{
		Sentiment: result.Choice(lunaQSentiment).Value,
		EventType: result.Choice(lunaQEventType).Value,
		Summary:   summary,
	}, nil
}
