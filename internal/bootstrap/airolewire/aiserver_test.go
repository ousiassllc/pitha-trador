package airolewire_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// hit is what a mock AI server received: the path, the Authorization header
// (empty when none came) and the model name of the body. The keys are fakes.
type hit struct{ path, auth, model string }

// AIServer records every request and answers 400 (never retried), so a client's
// request is seen exactly once and nothing has to be parsed.
type AIServer struct {
	mu   sync.Mutex
	hits []hit
	URL  string
}

func NewAIServer(t *testing.T) *AIServer {
	t.Helper()
	s := &AIServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.hits = append(s.hits, hit{r.URL.Path, r.Header.Get("Authorization"), body.Model})
		s.mu.Unlock()
		http.Error(w, "recorded", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

// Expect fails unless the requests since the last call are exactly want.
func (s *AIServer) Expect(t *testing.T, what string, want ...hit) {
	t.Helper()
	s.mu.Lock()
	got := s.hits
	s.hits = nil
	s.mu.Unlock()
	if len(got) != len(want) {
		t.Errorf("%s: %d requests %v, want %d %v", what, len(got), got, len(want), want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: request %d = %+v, want %+v", what, i, got[i], want[i])
		}
	}
}
