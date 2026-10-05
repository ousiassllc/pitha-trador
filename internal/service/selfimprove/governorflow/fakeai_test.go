package governorflow_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

// fakeAI stands in for the external Sol and Opus LLM APIs: solBody/
// opusBody are their raw JSON answers (opusBody's default approves), and
// solCalls/opusCalls count requests. A non-zero *Status makes that API
// fail with that HTTP status.
type fakeAI struct {
	mu         sync.Mutex
	solBody    string
	opusBody   string
	solStatus  int
	opusStatus int
	solCalls   int
	opusCalls  int
	sol        *assist.Sol
	opus       *assist.Opus
}

const (
	solProposesLongMinProbability065 = `{"rationale":{"why":"weak 0.60-0.70 bucket"},"proposed_changes":[{"key":"policy.long.min_probability","new_value":0.65}]}`
	opusApproves                     = `{"verdict":"approve","reason":"qualitatively sound"}`
)

func newFakeAI(t *testing.T) *fakeAI {
	t.Helper()
	f := &fakeAI{solBody: `{"proposed_changes":[]}`, opusBody: opusApproves}

	serve := func(calls *int, status *int, body *string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			*calls++
			st, b := *status, *body
			f.mu.Unlock()
			if st != 0 {
				http.Error(w, "down", st)
				return
			}
			_, _ = w.Write([]byte(b))
		}
	}
	solServer := httptest.NewServer(serve(&f.solCalls, &f.solStatus, &f.solBody))
	opusServer := httptest.NewServer(serve(&f.opusCalls, &f.opusStatus, &f.opusBody))
	t.Cleanup(solServer.Close)
	t.Cleanup(opusServer.Close)

	f.sol = assist.NewSol(assist.NewClient(assist.Config{Label: "sol", BaseURL: solServer.URL, MaxAttempts: 1}))
	f.opus = assist.NewOpus(assist.NewClient(assist.Config{Label: "opus", BaseURL: opusServer.URL, MaxAttempts: 1}))
	return f
}

func (f *fakeAI) options(opts ...selfimprove.Option) []selfimprove.Option {
	return append([]selfimprove.Option{selfimprove.WithSol(f.sol), selfimprove.WithOpus(f.opus)}, opts...)
}

func (f *fakeAI) calls() (sol, opus int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.solCalls, f.opusCalls
}

// recordingNotifier records AIStageSkipped notifications.
type recordingNotifier struct {
	selfimprove.NoopNotifier
	mu      sync.Mutex
	skipped []string
}

func (n *recordingNotifier) AIStageSkipped(_ context.Context, stage string, _ error) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.skipped = append(n.skipped, stage)
	return nil
}
