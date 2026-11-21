package disclosureskiptests

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/disclosureskip"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

type lunaStub struct{}

func (lunaStub) Classify(_ context.Context, item assist.NewsItem) (assist.Classification, error) {
	return assist.Classification{Sentiment: domain.NewsSentimentBullish, EventType: domain.NewsEventEarnings, Summary: "S:" + item.Headline}, nil
}

type oneSymbol struct{}

func (oneSymbol) NewsSymbols(context.Context) ([]string, error) { return []string{sym}, nil }

// News Ingest sees the same feed with and without the recorder, whatever the
// recording does: same news_context, same news flag, same TDnet score, and so
// the Policy Engine decides the same.
func TestRecordingFeed_NewsIngestAndPolicyDecideTheSameWithAndWithoutRecording(t *testing.T) {
	items := []assist.NewsItem{
		tdnet("a", tanshin, feedNow.Add(-time.Hour)),
		tdnet("b", "配当予想の修正に関するお知らせ", feedNow.Add(-2*time.Hour)),
	}
	for _, tc := range []struct {
		name string
		sink disclosureskip.Sink
		size int
	}{
		{"healthy writer", newCollector(), disclosureskip.DefaultQueueSize},
		{"failing writer", sinkFunc(func(context.Context, disclosureskip.FetchEvent) error { return errors.New("db down") }), disclosureskip.DefaultQueueSize},
		{"panicking writer", sinkFunc(func(context.Context, disclosureskip.FetchEvent) error { panic("boom") }), disclosureskip.DefaultQueueSize},
		{"queue of one", newCollector(), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newService := func(feed newsfeed.Feed) *newsfeed.Service {
				return newsfeed.NewService(feed, lunaStub{}, oneSymbol{}, newsfeed.WithNow(func() time.Time { return feedNow }))
			}
			plain := newService(&scriptedFeed{items: map[string][]assist.NewsItem{sym: items}})
			recorded := disclosureskip.NewRecordingFeed(&scriptedFeed{items: map[string][]assist.NewsItem{sym: items}}, newsfeed.YanoshinLimit, tc.sink, disclosureskip.WithQueueSize(tc.size))
			withRecorder := newService(recorded)
			runWriter(t, recorded)

			for range 3 { // three poll cycles
				if err := plain.Poll(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := withRecorder.Poll(context.Background()); err != nil {
					t.Fatal(err)
				}
			}

			wantCtx, wantOK := plain.NewsContext(sym)
			gotCtx, gotOK := withRecorder.NewsContext(sym)
			if gotOK != wantOK || !reflect.DeepEqual(gotCtx, wantCtx) {
				t.Fatalf("NewsContext = %+v, %v; without the recorder %+v, %v", gotCtx, gotOK, wantCtx, wantOK)
			}
			if got, want := withRecorder.TakeNewsFlag(sym), plain.TakeNewsFlag(sym); got != want || !want {
				t.Fatalf("TakeNewsFlag = %v, without the recorder %v (want true)", got, want)
			}
			if got, want := withRecorder.TDnetScore(sym, feedNow), plain.TDnetScore(sym, feedNow); got != want || want == 0 {
				t.Fatalf("TDnetScore = %v, without the recorder %v (want non-zero)", got, want)
			}

			// The same Jev answer through the Policy Engine (read only: Decide
			// does no I/O besides the Risk checker) decides the same.
			decide := func(svc *newsfeed.Service) domain.TradeSignal {
				score := svc.TDnetScore(sym, feedNow)
				return policy.NewEngine(policyThresholds(), nil, nil).Decide(context.Background(), policy.Input{
					InstrumentID: 1, Symbol: sym, Timestamp: feedNow, TDnetScore: &score,
					Decision:            &domain.JevDecision{ID: 7, Direction: ptr(domain.JevDirectionLong), EntryQuality: ptr(domain.JevEntryQualityStrong), Confidence: ptr(0.7), ContinuationProbability: ptr(0.7), ToxicFlow: ptr(0.1), LiquidityStressed: ptr(0.1)},
					SpreadBps:           ptr(10.0),
					EntryPriceReference: ptr(2110.5),
					Calibrated:          true,
				})
			}
			if got, want := decide(withRecorder), decide(plain); !reflect.DeepEqual(got, want) {
				t.Fatalf("Policy decided %+v, without the recorder %+v", got, want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func policyThresholds() policy.Thresholds {
	dir := config.PolicyDirectionThresholds{MinProbability: 0.68, MinEntryQuality: "strong", MinContinuationProbability: 0.6, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25}
	return policy.Thresholds{Policy: config.PolicyConfig{Long: dir, Short: dir}, MaxSpreadBps: 50, MinTurnover5mJPY: 3_000_000}
}
