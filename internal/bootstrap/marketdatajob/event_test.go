package marketdatajob

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

func eventTestBars(inst domain.Instrument, prevVWAPBps, currVWAPBps float64) (domain.Snapshot, []domain.Snapshot) {
	at := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)
	prev := domain.Snapshot{InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: at.Add(-time.Minute), Price: 2500,
		Feature: domain.Feature{PriceVsVWAPBps: prevVWAPBps}}
	curr := domain.Snapshot{InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: at, Price: 2500,
		Feature: domain.Feature{PriceVsVWAPBps: currVWAPBps}}
	return curr, []domain.Snapshot{prev}
}

func claimJevScout(t *testing.T, env testEnv) (jobqueue.Job, bool) {
	t.Helper()
	job, err := env.Jobs.ClaimNext(context.Background(), jobqueue.JobQueueJevScout, time.Date(2026, 9, 28, 2, 0, 0, 0, time.UTC))
	if errors.Is(err, jobqueue.ErrJobNotFound) {
		return jobqueue.Job{}, false
	}
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	return job, true
}

func TestEnqueueEventReevaluation_CandidateWithVWAPCrossEnqueuesJevScout(t *testing.T) {
	env := newTestEnv(t)
	inst := mustCreateInstrument(t, env, "7203")
	env.Screener.Set([]domain.Candidate{{InstrumentID: inst.ID, Symbol: inst.Symbol}}, time.Now().UTC())

	curr, history := eventTestBars(inst, -5, 5)
	if err := env.enqueueEventReevaluation(context.Background(), curr, history); err != nil {
		t.Fatalf("enqueueEventReevaluation: %v", err)
	}
	if _, ok := claimJevScout(t, env); !ok {
		t.Fatal("no jev-scout job enqueued, want an immediate re-evaluation for a VWAP cross (FR-SCAN-1)")
	}
}

func TestEnqueueEventReevaluation_QuietCandidateIsSuppressed(t *testing.T) {
	env := newTestEnv(t)
	inst := mustCreateInstrument(t, env, "7203")
	env.Screener.Set([]domain.Candidate{{InstrumentID: inst.ID, Symbol: inst.Symbol}}, time.Now().UTC())

	curr, history := eventTestBars(inst, 5, 5)
	if err := env.enqueueEventReevaluation(context.Background(), curr, history); err != nil {
		t.Fatalf("enqueueEventReevaluation: %v", err)
	}
	if job, ok := claimJevScout(t, env); ok {
		t.Fatalf("jev-scout job %+v enqueued for a quiet bar, want FR-SCAN-2 suppression", job)
	}
}

func TestEnqueueEventReevaluation_NonCandidateIsSkipped(t *testing.T) {
	env := newTestEnv(t)
	inst := mustCreateInstrument(t, env, "7203")

	curr, history := eventTestBars(inst, -5, 5)
	if err := env.enqueueEventReevaluation(context.Background(), curr, history); err != nil {
		t.Fatalf("enqueueEventReevaluation: %v", err)
	}
	if job, ok := claimJevScout(t, env); ok {
		t.Fatalf("jev-scout job %+v enqueued for a non-candidate, want none", job)
	}
}

// newsTestHandler builds a Handler whose News Ingest talks to fake Luna
// and news-feed servers, so a real Poll fills the news cache (issue #81).
func newsTestEnv(t *testing.T, lunaStatus int) testEnv {
	t.Helper()
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
			{"id": "n1", "headline": "上方修正", "body": "本文", "published_at": time.Now().UTC()},
		}})
	}))
	t.Cleanup(feed.Close)
	luna := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if lunaStatus != http.StatusOK {
			http.Error(w, "down", lunaStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"sentiment": "bullish", "event_type": "業績修正", "summary": "上方修正"})
	}))
	t.Cleanup(luna.Close)

	return newTestEnvWithNews(t,
		newsfeed.FeedConfig{URL: feed.URL, APIKey: "feed-key"},
		assist.Config{Label: "luna", BaseURL: luna.URL, APIKey: "luna-key"})
}

func TestEnqueueEventReevaluation_NewsFlagTriggersQuietCandidate(t *testing.T) {
	env := newsTestEnv(t, http.StatusOK)
	inst := mustCreateInstrument(t, env, "7203")
	env.Screener.Set([]domain.Candidate{{InstrumentID: inst.ID, Symbol: inst.Symbol}}, time.Now().UTC())
	if err := env.News.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	quiet, history := eventTestBars(inst, 5, 5)
	if err := env.enqueueEventReevaluation(context.Background(), quiet, history); err != nil {
		t.Fatalf("enqueueEventReevaluation: %v", err)
	}
	if _, ok := claimJevScout(t, env); !ok {
		t.Fatal("no jev-scout job enqueued for a quiet bar with a news flag, want FR-SCAN-1's ニュースフラグ trigger")
	}

	// The flag is consumed: the next quiet bar is suppressed again (FR-SCAN-2).
	if err := env.enqueueEventReevaluation(context.Background(), quiet, history); err != nil {
		t.Fatalf("enqueueEventReevaluation: %v", err)
	}
	if job, ok := claimJevScout(t, env); ok {
		t.Fatalf("jev-scout job %+v enqueued again for the same news, want none", job)
	}
}

func TestEnqueueEventReevaluation_LunaFailureRaisesNoNewsFlag(t *testing.T) {
	env := newsTestEnv(t, http.StatusInternalServerError)
	inst := mustCreateInstrument(t, env, "7203")
	env.Screener.Set([]domain.Candidate{{InstrumentID: inst.ID, Symbol: inst.Symbol}}, time.Now().UTC())
	if err := env.News.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	quiet, history := eventTestBars(inst, 5, 5)
	if err := env.enqueueEventReevaluation(context.Background(), quiet, history); err != nil {
		t.Fatalf("enqueueEventReevaluation: %v", err)
	}
	if job, ok := claimJevScout(t, env); ok {
		t.Fatalf("jev-scout job %+v enqueued although Luna failed, want the normal flow untouched (FR-LUNA-4)", job)
	}
}
