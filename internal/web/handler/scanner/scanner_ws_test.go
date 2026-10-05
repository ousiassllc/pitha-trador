package scanner_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/scanner"
)

func TestScannerHandler_WebSocket_PushesScannerUpdateMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	asOf := time.Date(2026, 9, 26, 10, 15, 0, 0, time.FixedZone("JST", 9*60*60))
	source := scanner.StaticCandidateSource{Items: fixtureCandidates(), AsOf: asOf}
	h := scanner.NewScannerHandler(source, scanner.CandidateRefreshInterval{
		Min: 20 * time.Millisecond, Max: 30 * time.Millisecond,
	})

	engine := gin.New()
	engine.GET("/ws/scanner", h.WebSocket)
	server := httptest.NewServer(engine)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/scanner"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	var got struct {
		Type  string `json:"type"`
		AsOf  string `json:"as_of"`
		Items []struct {
			Symbol    string `json:"symbol"`
			DetailURL string `json:"detail_url"`
		} `json:"items"`
	}

	// First push happens immediately on connect; a second push follows
	// within the configured 20-30ms interval, proving the handler loops
	// rather than pushing once and stopping.
	for i := 0; i < 2; i++ {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("conn.Read() [%d] error = %v", i, err)
		}
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("json.Unmarshal() [%d] error = %v, data = %s", i, err, data)
		}
		if got.Type != "scanner_update" {
			t.Fatalf("message[%d].Type = %q, want %q", i, got.Type, "scanner_update")
		}
		if len(got.Items) != 2 || got.Items[0].Symbol != "7203" {
			t.Fatalf("message[%d].Items = %+v, want fixtureCandidates()", i, got.Items)
		}
		if got.Items[0].DetailURL != "/symbols/7203" {
			t.Fatalf("message[%d].Items[0].detail_url = %q, want %q", i, got.Items[0].DetailURL, "/symbols/7203")
		}
		// Same RFC 3339 string (offset preserved) that GET /api/v1/scanner
		// and the SSR caption show, not a UTC "Z" conversion.
		if want := "2026-09-26T10:15:00+09:00"; got.AsOf != want {
			t.Fatalf("message[%d].as_of = %q, want %q", i, got.AsOf, want)
		}
	}

	_ = conn.Close(websocket.StatusNormalClosure, "")
}

// TestWebSocket_ClientCloseEndsHandlerPromptly guards issue #127: the
// handler must end as soon as the client closes, rather than waiting out its
// (here: one hour) polling interval for the next write to fail.
func TestWebSocket_ClientCloseEndsHandlerPromptly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const longInterval = time.Hour

	h := scanner.NewScannerHandler(
		scanner.StaticCandidateSource{Items: fixtureCandidates(), AsOf: time.Now()},
		scanner.CandidateRefreshInterval{Min: longInterval, Max: longInterval},
	)

	returned := make(chan struct{})
	engine := gin.New()
	engine.GET("/ws/scanner", func(c *gin.Context) {
		defer close(returned)
		h.WebSocket(c)
	})
	server := httptest.NewServer(engine)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/scanner"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Complete the handshake with the handler idling in its interval wait
	// before closing (CloseRead is what lets the server see the Close frame
	// at all).
	time.Sleep(50 * time.Millisecond)
	go func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	// Drain so the client side can finish the close handshake.
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()

	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatalf("handler still running 3s after client close (interval %v)", longInterval)
	}
}

// flakyCandidateSource fails its first failures Candidates calls.
type flakyCandidateSource struct {
	failures int64
	calls    atomic.Int64
}

func (f *flakyCandidateSource) Candidates(context.Context) ([]domain.Candidate, time.Time, error) {
	if f.calls.Add(1) <= f.failures {
		return nil, time.Time{}, errors.New("database is locked")
	}
	return fixtureCandidates(), time.Now(), nil
}

func dialScannerWS(t *testing.T, source scanner.CandidateSource) (context.Context, *websocket.Conn) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := scanner.NewScannerHandler(source, scanner.CandidateRefreshInterval{Min: 10 * time.Millisecond, Max: 10 * time.Millisecond})
	engine := gin.New()
	engine.GET("/ws/scanner", h.WebSocket)
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/scanner", nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return ctx, conn
}

// Issue #266: a failed Candidates read must not drop the connection.
func TestScannerHandler_WebSocket_SourceErrorKeepsConnectionOpen(t *testing.T) {
	ctx, conn := dialScannerWS(t, &flakyCandidateSource{failures: 2})

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("conn.Read() error = %v, want the scanner_update pushed after the transient errors", err)
	}
	if !strings.Contains(string(data), `"scanner_update"`) {
		t.Fatalf("message = %s, want scanner_update", data)
	}
}

func TestScannerHandler_WebSocket_PersistentSourceErrorClosesConnection(t *testing.T) {
	ctx, conn := dialScannerWS(t, &flakyCandidateSource{failures: 1 << 30})

	if _, _, err := conn.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("conn.Read() error = %v (ctx err %v), want the server to give up and close", err, ctx.Err())
	}
}
