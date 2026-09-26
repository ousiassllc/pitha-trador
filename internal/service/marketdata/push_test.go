package marketdata_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

func TestPushClient_Run_DeliversMessagesAndMarksFresh(t *testing.T) {
	messages := []marketdata.Board{
		{Symbol: "7203", CurrentPrice: 2409.0},
		{Symbol: "7203", CurrentPrice: 2410.5},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

		for _, msg := range messages {
			data, err := json.Marshal(msg)
			if err != nil {
				t.Errorf("marshal push message: %v", err)
				return
			}
			if err := conn.Write(r.Context(), websocket.MessageText, data); err != nil {
				return
			}
		}
		// Keep the connection open briefly so the client has time to read
		// both messages before Run's ctx is canceled by the test.
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	status := marketdata.NewStatusTracker()
	client := marketdata.NewPushClient(wsURL, status)

	var received []marketdata.Board
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- client.Run(ctx, func(b marketdata.Board) {
			received = append(received, b)
		})
	}()

	deadline := time.After(1 * time.Second)
	for len(received) < len(messages) {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for push messages, got %d of %d", len(received), len(messages))
		case <-time.After(5 * time.Millisecond):
		}
	}

	if len(received) != len(messages) {
		t.Fatalf("received %d messages, want %d", len(received), len(messages))
	}
	if received[0].CurrentPrice != 2409.0 || received[1].CurrentPrice != 2410.5 {
		t.Errorf("received = %+v", received)
	}

	if status.IsStale("7203") {
		t.Error("symbol marked stale after receiving push messages")
	}

	cancel()
	<-done
}

func TestPushClient_Run_ReturnsErrorOnDialFailure(t *testing.T) {
	client := marketdata.NewPushClient("ws://127.0.0.1:1/no-such-server", marketdata.NewStatusTracker())
	if err := client.Run(context.Background(), func(marketdata.Board) {}); err == nil {
		t.Fatal("Run: want error dialing an unreachable server, got nil")
	}
}
