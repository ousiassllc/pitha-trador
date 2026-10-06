// Package slotflow holds the external test of REST registration-slot
// rotation: kabuステーション registers every symbol requested through an
// information API and caps the list at 50 (REST and PUSH together), so
// polling more distinct symbols than that must free slots. It lives in its
// own directory to keep internal/service/marketdata under the per-directory
// line limit.
package slotflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// slotServer mimics the API登録銘柄リスト: /board and /symbol register the
// symbol (4002006 when the list is full), /register adds, /unregister and
// /unregister/all remove.
type slotServer struct {
	*httptest.Server
	mu          sync.Mutex
	capacity    int
	registered  map[string]bool
	unregisters int
}

func newSlotServer(t *testing.T, capacity int) *slotServer {
	t.Helper()
	s := &slotServer{capacity: capacity, registered: map[string]bool{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		path := r.URL.Path
		switch path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
		case "/unregister/all":
			s.registered = map[string]bool{}
			_ = json.NewEncoder(w).Encode(map[string]any{"RegistList": []any{}})
		case "/unregister", "/register":
			var req struct{ Symbols []marketdata.RegisterSymbol }
			_ = json.NewDecoder(r.Body).Decode(&req)
			for _, sym := range req.Symbols {
				if path == "/register" {
					s.registered[sym.Symbol] = true
				} else {
					delete(s.registered, sym.Symbol)
				}
			}
			if path == "/unregister" {
				s.unregisters++
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"RegistList": []any{}})
		default: // /board/{symbol}@1, /symbol/{symbol}@1
			symbol, _, _ := strings.Cut(path[strings.LastIndex(path, "/")+1:], "@")
			if !s.registered[symbol] && len(s.registered) >= s.capacity {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"Code": 4002006, "Message": "レジスト数エラー"})
				return
			}
			s.registered[symbol] = true
			_ = json.NewEncoder(w).Encode(map[string]any{"Symbol": symbol, "CurrentPrice": 100.0})
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *slotServer) snapshot() (registered []string, unregisters int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sym := range s.registered {
		registered = append(registered, sym)
	}
	return registered, s.unregisters
}

func newClient(t *testing.T, s *slotServer) *marketdata.Client {
	t.Helper()
	client := marketdata.NewClient(marketdata.Config{BaseURL: s.URL, APIPassword: "pw", InfoAPIMaxPerSecond: 10})
	if _, err := client.IssueToken(context.Background()); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	return client
}

// More distinct symbols than free slots are all fetched: a full list frees
// the REST-registered symbols in one request and retries.
func TestGetBoard_RotatesRegistrationSlots(t *testing.T) {
	server := newSlotServer(t, 3)
	client := newClient(t, server)
	ctx := context.Background()

	for i := range 10 {
		symbol := fmt.Sprintf("%04d", 1000+i)
		if _, err := client.GetBoard(ctx, symbol, marketdata.ExchangeTSE); err != nil {
			t.Fatalf("GetBoard(%s): %v", symbol, err)
		}
	}
	_, unregisters := server.snapshot()
	if unregisters == 0 || unregisters > 4 {
		t.Errorf("/unregister calls = %d, want a few batched releases (not one per symbol)", unregisters)
	}
}

// Symbols registered for PUSH are never released by the REST rotation.
func TestGetBoard_RotationKeepsPushSymbols(t *testing.T) {
	server := newSlotServer(t, 4)
	client := newClient(t, server)
	ctx := context.Background()
	push := []marketdata.RegisterSymbol{{Symbol: "7203", Exchange: 1}, {Symbol: "6758", Exchange: 1}, {Symbol: "9984", Exchange: 1}}
	if _, err := client.RegisterSymbols(ctx, push); err != nil {
		t.Fatalf("RegisterSymbols: %v", err)
	}

	for i := range 6 { // one free slot: every new symbol evicts the previous REST one
		symbol := fmt.Sprintf("%04d", 1000+i)
		if _, err := client.GetBoard(ctx, symbol, marketdata.ExchangeTSE); err != nil {
			t.Fatalf("GetBoard(%s): %v", symbol, err)
		}
	}
	registered, _ := server.snapshot()
	have := map[string]bool{}
	for _, s := range registered {
		have[s] = true
	}
	for _, p := range push {
		if !have[p.Symbol] {
			t.Errorf("PUSH symbol %s was unregistered by the REST rotation", p.Symbol)
		}
	}
}

// Slots held by something other than this Client (PUSH or the operator)
// cannot be freed: the original 4002006 is returned.
func TestGetBoard_FullListWithoutReleasableSymbolsFails(t *testing.T) {
	server := newSlotServer(t, 1)
	server.registered["7203"] = true
	client := newClient(t, server)

	_, err := client.GetBoard(context.Background(), "6758", marketdata.ExchangeTSE)
	var apiErr *marketdata.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.Code != 4002006 {
		t.Fatalf("GetBoard err = %v, want the 4002006 APIError", err)
	}
}

// UnregisterAll forgets what was registered: a previous run's leftovers
// stop occupying slots, and the next REST symbols fit again.
func TestUnregisterAll_FreesSlotsLeftByPreviousRun(t *testing.T) {
	server := newSlotServer(t, 2)
	server.registered["1111"] = true
	server.registered["2222"] = true
	client := newClient(t, server)
	ctx := context.Background()

	if _, err := client.GetBoard(ctx, "3333", marketdata.ExchangeTSE); err == nil {
		t.Fatal("GetBoard succeeded with a full list of unknown registrations")
	}
	if err := client.UnregisterAll(ctx); err != nil {
		t.Fatalf("UnregisterAll: %v", err)
	}
	if _, err := client.GetBoard(ctx, "3333", marketdata.ExchangeTSE); err != nil {
		t.Fatalf("GetBoard after UnregisterAll: %v", err)
	}
}

// UnregisterSymbols frees only the given symbols and tolerates ones kabu no
// longer holds; the ranking watch list uses it to drop symbols it rotated out.
func TestUnregisterSymbols_FreesGivenSymbolsOnly(t *testing.T) {
	server := newSlotServer(t, 50)
	client := newClient(t, server)
	ctx := context.Background()
	watch := []marketdata.RegisterSymbol{{Symbol: "7203", Exchange: 1}, {Symbol: "6758", Exchange: 1}}
	if _, err := client.RegisterSymbols(ctx, watch); err != nil {
		t.Fatalf("RegisterSymbols: %v", err)
	}
	if err := client.UnregisterSymbols(ctx, nil); err != nil {
		t.Fatalf("UnregisterSymbols(nil): %v", err)
	}
	if err := client.UnregisterSymbols(ctx, watch[:1]); err != nil {
		t.Fatalf("UnregisterSymbols: %v", err)
	}
	registered, _ := server.snapshot()
	if len(registered) != 1 || registered[0] != "6758" {
		t.Errorf("registered = %v, want only 6758", registered)
	}
}
