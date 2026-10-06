package jev_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

func TestAPIError_ErrorLengthIndependentOfBodySize(t *testing.T) {
	body := "<html>\n<body>" + strings.Repeat("障害 ", 300_000) + "</body></html>"
	err := &jev.APIError{StatusCode: http.StatusBadGateway, Body: body}

	msg := err.Error()
	if len(msg) > 300 {
		t.Fatalf("Error() length = %d, want a bounded excerpt (<= 300)", len(msg))
	}
	if strings.ContainsAny(msg, "\r\n") || !utf8.ValidString(msg) {
		t.Fatalf("Error() = %q, want single-line valid UTF-8", msg)
	}
	if !strings.Contains(msg, "status 502") {
		t.Fatalf("Error() = %q, want the status code kept", msg)
	}
}

func TestClient_Scout_HugeErrorPageKeepsErrorMessageBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(strings.Repeat("<p>error</p>\n", 100_000)))
	}))
	defer server.Close()

	client := jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 1})
	_, _, err := client.Scout(context.Background(), jev.ScoutRequest{State: jev.ScoutState{Symbol: "1301"}})
	var apiErr *jev.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Scout err = %v, want *jev.APIError", err)
	}
	if len(err.Error()) > 400 {
		t.Fatalf("err length = %d, want <= 400 (excerpt plus wrapping)", len(err.Error()))
	}
}
