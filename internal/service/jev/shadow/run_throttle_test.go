package shadow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevshadow"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// noSpend is a Results with nothing spent (a 429 never reaches Insert).
type noSpend struct{}

func (noSpend) Insert(context.Context, jevshadow.Decision) (jevshadow.Decision, error) {
	return jevshadow.Decision{}, nil
}

func (noSpend) SpentBetween(context.Context, time.Time, time.Time) (judgement.Spend, error) {
	return judgement.Spend{}, nil
}

// The shadow's own 429 stops it for the cooldown even when the throttle record
// cannot be written (runtime_settings failing): the next comparisons send
// nothing, and it sends again after the cooldown.
func TestRun_StopsOnItsOwn429EvenWhenTheRecordCannotBeWritten(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	store := &memSettings{failSet: true, values: map[string]string{
		jevshadowsettings.KeyEnabled: "1", jevshadowsettings.KeyCandidateVersion: `"trader-v4"`,
	}}
	now := time.Date(2026, 10, 22, 1, 0, 0, 0, time.UTC)
	client := jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 4, RetryBaseDelay: time.Millisecond, Now: func() time.Time { return now }})
	s := New(client, noSpend{}, store, metalabelsettings.ModePaper, func() time.Time { return now }, WithMinInterval(0))
	compare := func() {
		s.Submit(domain.JevDecision{ID: 1, Symbol: "7203", QuestionVersion: "trader-v3"}, jev.ScoutState{Symbol: "7203", Timestamp: now, Price: 2100}, rag.Context{})
		s.Wait()
	}

	compare()
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests after the first comparison = %d, want 1 (no retry after a 429)", got)
	}
	for range 3 {
		compare()
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests inside the cooldown = %d, want still 1", got)
	}
	now = now.Add(jevshadowsettings.ThrottleCooldown)
	compare()
	if got := requests.Load(); got != 2 {
		t.Errorf("requests after the cooldown = %d, want 2", got)
	}
}
