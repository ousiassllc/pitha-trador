package assist_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

// stubAsker is a Jev-protocol backend that only counts its calls.
type stubAsker struct{ calls int }

func (*stubAsker) Configured() bool { return true }
func (s *stubAsker) Ask(context.Context, string, any, map[string]systemone.Question) (systemone.Result, error) {
	s.calls++
	return systemone.Result{}, errors.New("stub")
}

// WithBackend is asked on every call: a role is served by whichever backend it returns at that moment,
// is skipped (ErrNotConfigured, Configured false) when it returns an error, and recovers afterwards.
func TestWithBackendIsResolvedOnEveryCall(t *testing.T) {
	first, second := &stubAsker{}, &stubAsker{}
	current, fail := assist.Asker(first), false
	luna := assist.NewLuna(nil, assist.WithBackend(func(context.Context) (*assist.Client, assist.Asker, error) {
		if fail {
			return nil, nil, assist.ErrNotConfigured
		}
		return assist.NewClient(assist.Config{}), current, nil
	}))
	item := assist.NewsItem{Symbol: "7203", Headline: "h", PublishedAt: time.Now()}
	classify := func() error { _, err := luna.Classify(context.Background(), item); return err }

	_ = classify()
	current = second
	_ = classify()
	if first.calls != 1 || second.calls != 1 {
		t.Errorf("calls = %d, %d; want each backend used once", first.calls, second.calls)
	}

	fail = true
	if err := classify(); !errors.Is(err, assist.ErrNotConfigured) || luna.Configured() {
		t.Errorf("skipped role: err = %v, configured = %v", err, luna.Configured())
	}
	if first.calls != 1 || second.calls != 1 {
		t.Error("a skipped role still called a backend")
	}
	fail = false
	_ = classify()
	if second.calls != 2 || !luna.Configured() {
		t.Errorf("recovered role: calls = %d, configured = %v", second.calls, luna.Configured())
	}
}
