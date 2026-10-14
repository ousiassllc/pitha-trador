package newsfeed_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

// sentimentLuna classifies each article by its headline ("bullish",
// "bearish", "neutral" or anything else).
type sentimentLuna struct{}

func (sentimentLuna) Classify(_ context.Context, item assist.NewsItem) (assist.Classification, error) {
	return assist.Classification{Sentiment: item.Headline, EventType: domain.NewsEventOther}, nil
}

func scoreService(t *testing.T, items ...assist.NewsItem) *newsfeed.Service {
	t.Helper()
	feed := &fakeFeed{items: map[string][]assist.NewsItem{"7203": items}}
	svc := newsfeed.NewService(feed, sentimentLuna{}, fakeSymbols{"7203"},
		newsfeed.WithNow(func() time.Time { return t0 }), newsfeed.WithTTL(4*time.Hour))
	if err := svc.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	return svc
}

func near(got, want float64) bool { return math.Abs(got-want) < 1e-12 }

func TestTDnetScore_NoDisclosureIsNeutral(t *testing.T) {
	svc := scoreService(t)
	if got := svc.TDnetScore("7203", t0); got != newsfeed.TDnetScoreNeutral {
		t.Errorf("symbol with no disclosure = %v, want 0", got)
	}
	if got := svc.TDnetScore("9999", t0); got != 0 {
		t.Errorf("unknown symbol = %v, want 0", got)
	}
}

func TestTDnetScore_SignAndFreshnessWeight(t *testing.T) {
	cases := []struct {
		name  string
		items []assist.NewsItem
		at    time.Time
		want  float64
	}{
		{"fresh bullish", []assist.NewsItem{article("a", "bullish", 0)}, t0, 1},
		{"fresh bearish", []assist.NewsItem{article("a", "bearish", 0)}, t0, -1},
		{"neutral", []assist.NewsItem{article("a", "neutral", 0)}, t0, 0},
		{"unknown sentiment", []assist.NewsItem{article("a", "???", 0)}, t0, 0},
		{"half-aged bullish", []assist.NewsItem{article("a", "bullish", 2*time.Hour)}, t0, 0.5},
		{"aged by later at", []assist.NewsItem{article("a", "bullish", 0)}, t0.Add(time.Hour), 0.75},
		{"bullish and bearish cancel", []assist.NewsItem{article("a", "bullish", time.Hour), article("b", "bearish", time.Hour)}, t0, 0},
		{"fresh bearish outweighs old bullish", []assist.NewsItem{article("a", "bullish", 3*time.Hour), article("b", "bearish", 0)}, t0, -0.75},
		{"agreeing disclosures are clamped to 1", []assist.NewsItem{article("a", "bullish", 0), article("b", "bullish", time.Hour)}, t0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := scoreService(t, tc.items...)
			if got := svc.TDnetScore("7203", tc.at); !near(got, tc.want) {
				t.Errorf("TDnetScore = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTDnetScore_IgnoresFutureAndExpiredDisclosures(t *testing.T) {
	svc := scoreService(t, article("a", "bullish", time.Hour), article("b", "bullish", 5*time.Hour))
	// The 5h-old article is past the 4h TTL, so it is never cached.
	if got := svc.TDnetScore("7203", t0); !near(got, 0.75) {
		t.Errorf("TDnetScore = %v, want 0.75 (expired article ignored)", got)
	}
	// As of 2h before now, the article published 1h before now has not been
	// disclosed yet: no look-ahead.
	if got := svc.TDnetScore("7203", t0.Add(-2*time.Hour)); got != 0 {
		t.Errorf("TDnetScore before publication = %v, want 0", got)
	}
	// At the TTL boundary the weight reaches 0.
	if got := svc.TDnetScore("7203", t0.Add(3*time.Hour)); got != 0 {
		t.Errorf("TDnetScore at TTL = %v, want 0", got)
	}
}

func TestScoreNewsContext_MatchesCachedScore(t *testing.T) {
	// The training side rebuilds the score from the persisted news_context.
	svc := scoreService(t, article("a", "bullish", time.Hour), article("b", "bearish", 2*time.Hour))
	ctxNews, ok := svc.NewsContext("7203")
	if !ok {
		t.Fatal("NewsContext: no items")
	}
	got := newsfeed.ScoreNewsContext(ctxNews.Items, t0, 4*time.Hour)
	if want := svc.TDnetScore("7203", t0); !near(got, want) {
		t.Errorf("ScoreNewsContext = %v, TDnetScore = %v", got, want)
	}
	if got := newsfeed.ScoreNewsContext(nil, t0, 4*time.Hour); got != newsfeed.TDnetScoreNeutral {
		t.Errorf("ScoreNewsContext(nil) = %v, want 0", got)
	}
}
