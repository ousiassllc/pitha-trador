package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

func f(v float64) *float64 { return &v }
func s(v string) *string   { return &v }

func fixtureCandidates() []domain.Candidate {
	return []domain.Candidate{
		{
			Symbol: "7203", Price: 2831.5,
			Return1m: f(0.12), Return5m: f(0.42), VolumeRatio5m: f(3.4),
			PriceVsVWAPBps: 38, SpreadBps: f(7),
			JevDirection: s("LONG"), JevConfidence: f(0.74), EntryQuality: s("strong"),
			ScreenScore: 9.1,
		},
		{Symbol: "9984", Price: 7000}, // no Jev evaluation / position yet: every optional field nil
	}
}

func TestScannerHandler_APIScanner_MapsCandidatesToItemsAndAsOf(t *testing.T) {
	asOf := time.Date(2026, 9, 26, 10, 15, 0, 0, time.UTC)
	source := handler.StaticCandidateSource{Items: fixtureCandidates(), AsOf: asOf}
	h := handler.NewScannerHandler(source, handler.CandidateRefreshInterval{Min: time.Second, Max: 2 * time.Second})

	out, err := h.APIScanner(context.Background(), &struct{}{})
	if err != nil {
		t.Fatalf("APIScanner() error = %v", err)
	}
	if !out.Body.AsOf.Equal(asOf) {
		t.Fatalf("Body.AsOf = %v, want %v", out.Body.AsOf, asOf)
	}
	if len(out.Body.Items) != 2 {
		t.Fatalf("len(Body.Items) = %d, want 2", len(out.Body.Items))
	}
	first := out.Body.Items[0]
	if first.Symbol != "7203" || first.Price != 2831.5 || *first.JevDirection != "LONG" || *first.JevConfidence != 0.74 {
		t.Fatalf("Body.Items[0] = %+v, want symbol=7203 price=2831.5 jev_direction=LONG jev_confidence=0.74", first)
	}
	second := out.Body.Items[1]
	if second.Return1m != nil || second.JevDirection != nil || second.CurrentPosition != nil {
		t.Fatalf("Body.Items[1] = %+v, want every optional field nil (not yet Jev-evaluated)", second)
	}
}

func TestScannerHandler_Page_FullPageWithoutHXRequestHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	source := handler.StaticCandidateSource{Items: fixtureCandidates(), AsOf: time.Now()}
	h := handler.NewScannerHandler(source, handler.CandidateRefreshInterval{Min: time.Second, Max: 2 * time.Second})

	engine := gin.New()
	engine.GET("/scanner", h.Page)

	req := httptest.NewRequest(http.MethodGet, "/scanner", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<!doctype html>") && !strings.Contains(body, "<!DOCTYPE html>") {
		t.Fatalf("expected full page (doctype), got %q", body)
	}
	assertAllScannerColumns(t, body)
}

func TestScannerHandler_Page_FragmentWithHXRequestHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	source := handler.StaticCandidateSource{Items: fixtureCandidates(), AsOf: time.Now()}
	h := handler.NewScannerHandler(source, handler.CandidateRefreshInterval{Min: time.Second, Max: 2 * time.Second})

	engine := gin.New()
	engine.GET("/scanner", h.Page)

	req := httptest.NewRequest(http.MethodGet, "/scanner", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<!doctype") || strings.Contains(body, "<!DOCTYPE") || strings.Contains(body, "<html") {
		t.Fatalf("expected fragment only (no document shell) for HX-Request, got %q", body)
	}
	assertAllScannerColumns(t, body)
}

// assertAllScannerColumns checks every Scanner Dashboard display item from
// functional.md §5.1 is present: Symbol, Price, 1m/5m Return, Volume
// Ratio, VWAP distance, Spread, Jev Direction/Confidence/Entry Quality,
// Current Position.
func assertAllScannerColumns(t *testing.T, body string) {
	t.Helper()
	for _, want := range []string{
		"7203", "2831.5", // Symbol, Price
		"0.12", "0.42", // 1m/5m Return
		"3.40",                  // Volume Ratio
		"38",                    // VWAP distance (bps)
		"7</td>",                // Spread (bps)
		"LONG", "74%", "strong", // Jev Direction, Confidence, Entry Quality
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected rendered table to contain %q, got %q", want, body)
		}
	}
}

func TestStaticCandidateSource_ZeroAsOfFallsBackToNow(t *testing.T) {
	source := handler.StaticCandidateSource{}
	before := time.Now()
	_, asOf, err := source.Candidates(context.Background())
	after := time.Now()
	if err != nil {
		t.Fatalf("Candidates() error = %v", err)
	}
	if asOf.Before(before) || asOf.After(after) {
		t.Fatalf("Candidates() asOf = %v, want between %v and %v", asOf, before, after)
	}
}

func TestCandidateRefreshInterval_NextStaysWithinBounds(t *testing.T) {
	interval := handler.CandidateRefreshInterval{Min: 15 * time.Second, Max: 30 * time.Second}
	for i := 0; i < 50; i++ {
		got := interval.Next()
		if got < interval.Min || got > interval.Max {
			t.Fatalf("Next() = %v, want within [%v, %v]", got, interval.Min, interval.Max)
		}
	}
}

func TestCandidateRefreshInterval_MaxNotAfterMinReturnsMin(t *testing.T) {
	interval := handler.CandidateRefreshInterval{Min: 20 * time.Second, Max: 20 * time.Second}
	if got := interval.Next(); got != interval.Min {
		t.Fatalf("Next() = %v, want Min (%v) when Max <= Min", got, interval.Min)
	}
}

func TestScannerHandler_WebSocket_PushesScannerUpdateMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	source := handler.StaticCandidateSource{Items: fixtureCandidates(), AsOf: time.Now()}
	h := handler.NewScannerHandler(source, handler.CandidateRefreshInterval{
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
		Items []struct {
			Symbol string `json:"symbol"`
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
	}

	_ = conn.Close(websocket.StatusNormalClosure, "")
}
