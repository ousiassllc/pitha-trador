package clientflow_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// recordingTransport answers every request with scoutWireResponse and keeps
// the URL and model it saw, so a test can observe where an unconfigured
// Client connects without any network.
type recordingTransport struct {
	url   string
	model string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.url = req.URL.String()
	raw, _ := io.ReadAll(req.Body)
	var body struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(raw, &body)
	r.model = body.Model
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(scoutWireResponse)),
		Request:    req,
	}, nil
}

// Issues #271/#274: an empty BaseURL/Model falls back to the production host
// and the "jev-latest" alias; an explicit value wins over both defaults.
func TestClient_EmptyBaseURLAndModelUseDefaults(t *testing.T) {
	rt := &recordingTransport{}
	client := jev.NewClient(jev.Config{HTTPClient: &http.Client{Transport: rt}})

	if _, _, err := client.Scout(context.Background(), jev.ScoutRequest{}); err != nil {
		t.Fatalf("Scout: %v", err)
	}
	if want := "https://api.typesafe.ai/v1/systemone"; rt.url != want {
		t.Errorf("request URL = %q, want %q", rt.url, want)
	}
	if rt.model != "jev-latest" {
		t.Errorf("model = %q, want jev-latest", rt.model)
	}
}

func TestClient_ModelOverrideIsSentOnScoutAndTrader(t *testing.T) {
	server, got := captureServer(t, scoutWireResponse)
	client := jev.NewClient(jev.Config{BaseURL: server.URL, Model: "jev-custom"})

	if _, _, err := client.Scout(context.Background(), jev.ScoutRequest{}); err != nil {
		t.Fatalf("Scout: %v", err)
	}
	if got.Body.Model != "jev-custom" {
		t.Errorf("Scout model = %q, want jev-custom", got.Body.Model)
	}

	server, got = captureServer(t, traderWireResponse)
	client = jev.NewClient(jev.Config{BaseURL: server.URL, Model: "jev-custom"})
	if _, _, err := client.Trader(context.Background(), jev.TraderRequest{}); err != nil {
		t.Fatalf("Trader: %v", err)
	}
	if got.Body.Model != "jev-custom" {
		t.Errorf("Trader model = %q, want jev-custom", got.Body.Model)
	}
}
