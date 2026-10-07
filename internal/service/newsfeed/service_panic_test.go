package newsfeed_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

// panicFeed panics for panicSymbol and returns one article for the rest.
type panicFeed struct{ panicSymbol string }

func (f panicFeed) Fetch(_ context.Context, symbol string) ([]assist.NewsItem, error) {
	if symbol == f.panicSymbol {
		panic("feed boom")
	}
	return []assist.NewsItem{{ID: "a-" + symbol, Symbol: symbol, Headline: "h-" + symbol, PublishedAt: t0.Add(-time.Minute)}}, nil
}

// panicLuna panics when classifying panicSymbol's article.
type panicLuna struct {
	fakeLuna
	panicSymbol string
}

func (l *panicLuna) Classify(ctx context.Context, item assist.NewsItem) (assist.Classification, error) {
	if item.Symbol == l.panicSymbol {
		panic("classifier boom")
	}
	return l.fakeLuna.Classify(ctx, item)
}

type errorLog struct {
	mu      sync.Mutex
	symbols []string
}

func (l *errorLog) observe(symbol string, _ error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.symbols = append(l.symbols, symbol)
}

// A panic in one symbol's goroutine (feed or classifier) must not crash the
// process: Poll returns, the other symbols are still ingested and the
// failure reaches the ErrorObserver (FR-SCHED-6, FR-LUNA-4).
func TestPoll_PanickingSymbolDoesNotCrashAndOthersFinish(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cases := map[string]func(bad string) (newsfeed.Feed, newsfeed.Classifier){
		"feed": func(bad string) (newsfeed.Feed, newsfeed.Classifier) {
			return panicFeed{panicSymbol: bad}, &fakeLuna{}
		},
		"classifier": func(bad string) (newsfeed.Feed, newsfeed.Classifier) {
			return panicFeed{}, &panicLuna{panicSymbol: bad}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			feed, classifier := build("7203")
			errs := &errorLog{}
			svc := newsfeed.NewService(feed, classifier, fakeSymbols{"7203", "9984", "6758"},
				newsfeed.WithNow(func() time.Time { return t0 }),
				newsfeed.WithConcurrency(1),
				newsfeed.WithErrorObserver(errs.observe))

			if err := svc.Poll(context.Background()); err != nil {
				t.Fatalf("Poll returned %v, want nil", err)
			}
			for _, symbol := range []string{"9984", "6758"} {
				if _, ok := svc.NewsContext(symbol); !ok {
					t.Errorf("%s was not ingested after another symbol panicked", symbol)
				}
			}
			if _, ok := svc.NewsContext("7203"); ok {
				t.Error("panicking symbol must degrade to no news")
			}
			if len(errs.symbols) != 1 || errs.symbols[0] != "7203" {
				t.Errorf("observed errors = %v, want exactly [7203]", errs.symbols)
			}
		})
	}
}
