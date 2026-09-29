package assist_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

func lunaServer(t *testing.T, handler http.HandlerFunc) *assist.Luna {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := assist.NewClient(assist.Config{Label: "luna", BaseURL: server.URL, APIKey: "luna-key", MaxAttempts: 2, RetryBaseDelay: time.Millisecond})
	return assist.NewLuna(client)
}

func TestLuna_Classify_CallsExternalAPIWithBearerKey(t *testing.T) {
	var gotAuth, gotPath string
	var gotReq assist.LunaRequest
	luna := lunaServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_ = json.NewEncoder(w).Encode(assist.Classification{Sentiment: "bullish", EventType: "決算", Summary: "増益決算"})
	})

	got, err := luna.Classify(context.Background(), assist.NewsItem{Symbol: "7203", Headline: "トヨタ増益", Body: "本文"})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if got.Sentiment != domain.NewsSentimentBullish || got.EventType != domain.NewsEventEarnings || got.Summary != "増益決算" {
		t.Errorf("Classify = %+v, want Luna's answer", got)
	}
	if gotAuth != "Bearer luna-key" || gotPath != assist.LunaClassifyPath {
		t.Errorf("request auth/path = %q/%q, want Bearer luna-key at %s", gotAuth, gotPath, assist.LunaClassifyPath)
	}
	if gotReq.Symbol != "7203" || gotReq.Headline != "トヨタ増益" || gotReq.Body != "本文" {
		t.Errorf("request body = %+v, want the news item", gotReq)
	}
}

func TestLuna_Classify_RejectsMalformedClassification(t *testing.T) {
	tests := map[string]assist.Classification{
		"unknown sentiment":  {Sentiment: "moon", EventType: "決算", Summary: "x"},
		"unknown event type": {Sentiment: "bullish", EventType: "ゴシップ", Summary: "x"},
		"empty summary":      {Sentiment: "bullish", EventType: "決算"},
	}
	for name, resp := range tests {
		t.Run(name, func(t *testing.T) {
			luna := lunaServer(t, func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(resp) })
			if _, err := luna.Classify(context.Background(), assist.NewsItem{Symbol: "7203"}); err == nil {
				t.Errorf("Classify(%+v) succeeded, want an error", resp)
			}
		})
	}
}

func TestLuna_Classify_RetriesThenFailsOnServerError(t *testing.T) {
	var calls atomic.Int32
	luna := lunaServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	_, err := luna.Classify(context.Background(), assist.NewsItem{Symbol: "7203"})
	var apiErr *assist.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("Classify error = %v, want an *APIError with status 500", err)
	}
	if calls.Load() != 2 {
		t.Errorf("attempts = %d, want MaxAttempts (2)", calls.Load())
	}
}

func TestLuna_Classify_UnconfiguredFailsWithoutNetwork(t *testing.T) {
	luna := assist.NewLuna(assist.NewClient(assist.Config{Label: "luna"}))
	if _, err := luna.Classify(context.Background(), assist.NewsItem{Symbol: "7203"}); !errors.Is(err, assist.ErrNotConfigured) {
		t.Errorf("Classify error = %v, want ErrNotConfigured", err)
	}
}
