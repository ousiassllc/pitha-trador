package newsfeed_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/httpbody"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

// maxFeedBytes mirrors the unexported feed response cap (4 MiB).
const maxFeedBytes = 4 << 20

func feedWithBody(t *testing.T, body string) *newsfeed.FeedClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	t.Cleanup(server.Close)
	return newsfeed.NewFeedClient(newsfeed.FeedConfig{URL: server.URL})
}

func TestFeedClient_FetchOversizedResponseReturnsErrTooLarge(t *testing.T) {
	client := feedWithBody(t, strings.Repeat("a", maxFeedBytes+1))
	if _, err := client.Fetch(context.Background(), "7203"); !errors.Is(err, httpbody.ErrTooLarge) {
		t.Fatalf("err = %v, want httpbody.ErrTooLarge", err)
	}
}

func TestFeedClient_FetchResponseExactlyAtLimitIsRead(t *testing.T) {
	const prefix, suffix = `{"items":[{"id":"n1","headline":"`, `"}]}`
	client := feedWithBody(t, prefix+strings.Repeat("h", maxFeedBytes-len(prefix)-len(suffix))+suffix)
	items, err := client.Fetch(context.Background(), "7203")
	if err != nil {
		t.Fatalf("Fetch at the exact limit: %v", err)
	}
	if len(items) != 1 || items[0].ID != "n1" {
		t.Errorf("items = %d, want the single item", len(items))
	}
}

func TestFeedClient_FetchSendsSymbolAndKeyAndDecodesItems(t *testing.T) {
	var gotSymbol, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSymbol, gotAuth = r.URL.Query().Get("symbol"), r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
			{"id": "n1", "headline": "見出し", "body": "本文", "published_at": t0},
		}})
	}))
	t.Cleanup(server.Close)

	client := newsfeed.NewFeedClient(newsfeed.FeedConfig{URL: server.URL + "/news?lang=ja", APIKey: "feed-key"})
	items, err := client.Fetch(context.Background(), "7203")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotSymbol != "7203" || gotAuth != "Bearer feed-key" {
		t.Errorf("request symbol/auth = %q/%q", gotSymbol, gotAuth)
	}
	if len(items) != 1 || items[0].ID != "n1" || items[0].Symbol != "7203" || items[0].Headline != "見出し" || !items[0].PublishedAt.Equal(t0) {
		t.Errorf("items = %+v", items)
	}
}

func TestFeedClient_FetchFailsOnNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusBadGateway) }))
	t.Cleanup(server.Close)
	if _, err := newsfeed.NewFeedClient(newsfeed.FeedConfig{URL: server.URL}).Fetch(context.Background(), "7203"); err == nil {
		t.Error("Fetch succeeded on a 502, want an error")
	}
}
