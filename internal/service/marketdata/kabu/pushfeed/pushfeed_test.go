package pushfeed_test

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
	"time"

	"github.com/coder/websocket"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu/pushfeed"
)

type fakeUniverse struct{ stocks []domain.Instrument }

func (u fakeUniverse) ListActiveByKind(_ context.Context, kind string) ([]domain.Instrument, error) {
	if kind != domain.InstrumentKindStock {
		return nil, fmt.Errorf("unexpected kind %q", kind)
	}
	return u.stocks, nil
}

func stocks(n int) []domain.Instrument {
	out := make([]domain.Instrument, n)
	for i := range out {
		out[i] = domain.Instrument{Symbol: fmt.Sprintf("%04d", 1000+i), Kind: domain.InstrumentKindStock, IsActive: true}
	}
	return out
}

// fakeKabu serves REST token/register/board and a PUSH WebSocket that
// sends pushMessages then stays open.
type fakeKabu struct {
	rest, ws *httptest.Server
	client   *marketdata.Client
	mu       sync.Mutex
	regs     [][]marketdata.RegisterSymbol
	unregs   [][]marketdata.RegisterSymbol
	boards   int
	board    marketdata.Board
}

func newFakeKabu(t *testing.T, restBoard marketdata.Board, pushMessages ...marketdata.Board) *fakeKabu {
	t.Helper()
	f := &fakeKabu{board: restBoard}
	f.rest = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"ResultCode": 0, "Token": "tok"})
		case r.Method == http.MethodPut && r.URL.Path == "/unregister/all":
			_ = json.NewEncoder(w).Encode(marketdata.RegisterSuccess{})
		case r.Method == http.MethodPut && r.URL.Path == "/unregister":
			var req struct{ Symbols []marketdata.RegisterSymbol }
			_ = json.NewDecoder(r.Body).Decode(&req)
			f.mu.Lock()
			f.unregs = append(f.unregs, req.Symbols)
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(marketdata.RegisterSuccess{})
		case r.Method == http.MethodPut && r.URL.Path == "/register":
			var req struct{ Symbols []marketdata.RegisterSymbol }
			_ = json.NewDecoder(r.Body).Decode(&req)
			f.mu.Lock()
			f.regs = append(f.regs, req.Symbols)
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(marketdata.RegisterSuccess{RegistList: req.Symbols})
		case r.Method == http.MethodGet:
			f.mu.Lock()
			f.boards++
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(f.board)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	f.ws = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		for _, m := range pushMessages {
			data, _ := json.Marshal(m)
			if conn.Write(r.Context(), websocket.MessageText, data) != nil {
				return
			}
		}
		<-r.Context().Done()
	}))
	t.Cleanup(func() { f.rest.Close(); f.ws.Close() })
	f.client = marketdata.NewClient(marketdata.Config{BaseURL: f.rest.URL, APIPassword: "pw"})
	if _, err := f.client.IssueToken(context.Background()); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	return f
}

func (f *fakeKabu) wsURL() string { return "ws" + strings.TrimPrefix(f.ws.URL, "http") }

func (f *fakeKabu) registrations() [][]marketdata.RegisterSymbol {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]marketdata.RegisterSymbol(nil), f.regs...)
}

func (f *fakeKabu) boardFetches() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.boards
}

func TestRegisterUniverse_RegistersStocksUpToKabuCap(t *testing.T) {
	f := newFakeKabu(t, marketdata.Board{})
	feed := pushfeed.New(fakeUniverse{stocks(pushfeed.MaxRegisterSymbols + 3)}, f.client, f.wsURL(), 1)
	if err := feed.RegisterUniverse(context.Background()); err != nil {
		t.Fatalf("RegisterUniverse: %v", err)
	}
	regs := f.registrations()
	if len(regs) != 1 || len(regs[0]) != pushfeed.MaxRegisterSymbols {
		t.Fatalf("registrations = %v, want one call with %d symbols", regs, pushfeed.MaxRegisterSymbols)
	}
	if regs[0][0] != (marketdata.RegisterSymbol{Symbol: "1000", Exchange: 1}) {
		t.Errorf("first registered = %+v, want 1000@1", regs[0][0])
	}
}

// Run registers the universe, then PUSH boards reach Latest without a REST
// board poll (flows.md §10.1).
func TestRun_RegistersAndServesPushBoards(t *testing.T) {
	f := newFakeKabu(t, marketdata.Board{Symbol: "1000", CurrentPrice: 1},
		marketdata.Board{Symbol: "1000", CurrentPrice: 2555, VWAP: 2550})
	feed := pushfeed.New(fakeUniverse{stocks(1)}, f.client, f.wsURL(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { feed.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	deadline := time.Now().Add(5 * time.Second)
	for {
		board, err := feed.Latest(ctx, "1000")
		if err != nil {
			t.Fatalf("Latest: %v", err)
		}
		if board.Price == 2555 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("PUSH board never served; last price %v", board.Price)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if regs := f.registrations(); len(regs) == 0 || regs[0][0].Symbol != "1000" {
		t.Errorf("registrations = %v, want the universe registered", regs)
	}
}

// Issue #709: REST /board is only the thin supplement for PUSH-covered
// symbols. Once a PUSH board has arrived, repeated Latest calls (the 5〜15秒
// held-position loop and the per-minute market-data jobs) must be served from
// it without any further REST poll.
func TestLatest_FreshPushBoardNeedsNoRESTPoll(t *testing.T) {
	f := newFakeKabu(t, marketdata.Board{Symbol: "1000", CurrentPrice: 1},
		marketdata.Board{Symbol: "1000", CurrentPrice: 2555})
	feed := pushfeed.New(fakeUniverse{stocks(1)}, f.client, f.wsURL(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { feed.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	deadline := time.Now().Add(5 * time.Second)
	for {
		if b, err := feed.Latest(ctx, "1000"); err == nil && b.Price == 2555 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("PUSH board never served")
		}
		time.Sleep(10 * time.Millisecond)
	}
	before := f.boardFetches()
	for range 20 {
		if b, err := feed.Latest(ctx, "1000"); err != nil || b.Price != 2555 {
			t.Fatalf("Latest = (%+v, %v), want the PUSH board (2555)", b, err)
		}
	}
	if after := f.boardFetches(); after != before {
		t.Errorf("REST /board fetched %d times while a fresh PUSH board existed, want 0", after-before)
	}
}

func TestLatest_FallsBackToRESTWithoutPushBoard(t *testing.T) {
	f := newFakeKabu(t, marketdata.Board{Symbol: "1000", CurrentPrice: 2400})
	feed := pushfeed.New(fakeUniverse{}, f.client, f.wsURL(), 1)
	board, err := feed.Latest(context.Background(), "1000")
	if err != nil || board.Price != 2400 || f.boardFetches() != 1 {
		t.Fatalf("Latest = (%+v, %v), REST fetches %d; want the REST board (2400) after one fetch", board, err, f.boardFetches())
	}
}

// #173: a board whose CurrentPrice is 0/null (未約定・寄り付き前) is a
// missing price, never a price-0 bar.
func TestLatest_RejectsBoardWithoutCurrentPrice(t *testing.T) {
	f := newFakeKabu(t, marketdata.Board{Symbol: "1000", CurrentPrice: 0, VWAP: 0})
	feed := pushfeed.New(fakeUniverse{}, f.client, f.wsURL(), 1)
	if _, err := feed.Latest(context.Background(), "1000"); !errors.Is(err, broker.ErrPriceUnavailable) {
		t.Fatalf("Latest err = %v, want ErrPriceUnavailable", err)
	}
}

// panicOnceUniverse panics on its first ListActiveByKind call.
type panicOnceUniverse struct {
	fakeUniverse
	mu    sync.Mutex
	calls int
}

func (u *panicOnceUniverse) ListActiveByKind(ctx context.Context, kind string) ([]domain.Instrument, error) {
	u.mu.Lock()
	u.calls++
	first := u.calls == 1
	u.mu.Unlock()
	if first {
		panic("universe lookup blew up")
	}
	return u.fakeUniverse.ListActiveByKind(ctx, kind)
}

// FR-SCHED-6: a panic during a subscription attempt is logged and treated
// like any failed attempt: Run backs off and re-subscribes.
func TestRun_SurvivesPanicAndResubscribes(t *testing.T) {
	f := newFakeKabu(t, marketdata.Board{Symbol: "1000", CurrentPrice: 1})
	feed := pushfeed.New(&panicOnceUniverse{fakeUniverse: fakeUniverse{stocks(1)}}, f.client, f.wsURL(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { feed.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	deadline := time.Now().Add(10 * time.Second) // first retry backoff is 2s
	for len(f.registrations()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("Run never re-registered the universe after the panic")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
