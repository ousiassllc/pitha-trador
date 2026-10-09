package tachibana_test

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

func newClient(t *testing.T, fb *tt.FakeBroker, clk tachibana.Clock, perSecond int) *tachibana.Client {
	t.Helper()
	return tachibana.NewClient(tachibana.Config{BaseURL: fb.BaseURL(), RequestsPerSecond: perSecond, HTTPClient: fb.Server().Client(), Clock: clk})
}

func loginOK(t *testing.T, c *tachibana.Client, fb *tt.FakeBroker) {
	t.Helper()
	if _, err := c.Login(context.Background(), tt.AuthID, fb.Key()); err != nil {
		t.Fatalf("login: %v", err)
	}
}

func call(c *tachibana.Client, target tachibana.Target, prio tachibana.Priority) error {
	return c.Call(context.Background(), target, prio, "CLMTest", nil, nil)
}

func TestLoginEnvelopeAndVirtualURLs(t *testing.T) {
	clk := tt.NewAutoClock(time.Date(2026, 10, 8, 6, 0, 0, 123_000_000, tachibana.JST))
	fb := tt.New(t, clk)
	c := newClient(t, fb, clk, 10)

	loginOK(t, c, fb)
	if err := c.Call(context.Background(), tachibana.TargetPrice, tachibana.PriorityWatchQuote, "CLMTest", map[string]string{"k": "v"}, nil); err != nil {
		t.Fatal(err)
	}

	reqs := fb.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d", len(reqs))
	}
	login := reqs[0]
	if login.Method != http.MethodPost || login.Path != "/e_api_v4r10/auth/" {
		t.Errorf("login = %s %s, want POST /e_api_v4r10/auth/", login.Method, login.Path)
	}
	for k, want := range map[string]string{"sCLMID": "CLMAuthLoginRequest", "sAuthId": tt.AuthID, "sJsonOfmt": "4", "p_no": "1", "p_sd_date": "2026.10.08-06:00:00.123"} {
		if login.Body[k] != want {
			t.Errorf("login %s = %q, want %q", k, login.Body[k], want)
		}
	}
	next := reqs[1]
	if next.Method != http.MethodPost || !strings.HasPrefix(next.Path, "/"+tt.SecretTag+"/price/1") {
		t.Errorf("next = %s %s, want POST to the decrypted PRICE URL", next.Method, next.Path)
	}
	if next.Body["p_no"] != "2" || next.Body["k"] != "v" || next.Body["sJsonOfmt"] != "4" {
		t.Errorf("next body = %v", next.Body)
	}
	if !regexp.MustCompile(`^\d{4}\.\d{2}\.\d{2}-\d{2}:\d{2}:\d{2}\.\d{3}$`).MatchString(next.Body["p_sd_date"]) {
		t.Errorf("p_sd_date %q is not YYYY.MM.DD-HH:MM:SS.TTT", next.Body["p_sd_date"])
	}
}

func TestPSDDateIsJSTWhateverTheClockZone(t *testing.T) {
	utc := time.Date(2026, 10, 7, 21, 0, 5, 7_000_000, time.UTC) // 06:00:05.007 JST next day
	if got := tachibana.FormatSDDate(utc); got != "2026.10.08-06:00:05.007" {
		t.Errorf("formatSDDate = %q", got)
	}
}

func TestPNoResetsOnRelogin(t *testing.T) {
	clk := tt.NewAutoClock(tt.AtJST(2026, 10, 8, 6, 0))
	fb := tt.New(t, clk)
	c := newClient(t, fb, clk, 10)
	loginOK(t, c, fb)
	for range 3 {
		if err := call(c, tachibana.TargetRequest, tachibana.PriorityWatchQuote); err != nil {
			t.Fatal(err)
		}
	}
	loginOK(t, c, fb)
	var pnos []string
	for _, r := range fb.Requests() {
		pnos = append(pnos, r.Body["p_no"])
	}
	if got, want := strings.Join(pnos, ","), "1,2,3,4,1"; got != want {
		t.Errorf("p_no sequence = %s, want %s", got, want)
	}
}

// Concurrent callers across REQUEST/MASTER/PRICE never overlap, and the p_no
// order equals the arrival order at the server (run with -race).
func TestSerialQueueAcrossTargets(t *testing.T) {
	clk := tt.NewAutoClock(tt.AtJST(2026, 10, 8, 9, 0))
	fb := tt.New(t, clk)
	c := newClient(t, fb, clk, 10)
	loginOK(t, c, fb)

	var wg sync.WaitGroup
	targets := []tachibana.Target{tachibana.TargetRequest, tachibana.TargetMaster, tachibana.TargetPrice}
	for i := range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Call(context.Background(), targets[i%3], tachibana.PriorityWatchQuote, "CLMTest", map[string]string{"i": strconv.Itoa(i)}, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if got := fb.MaxInFlight(); got != 1 {
		t.Errorf("max requests in flight = %d, want 1", got)
	}
	last := 0
	for _, r := range fb.Requests() {
		n, _ := strconv.Atoi(r.Body["p_no"])
		if n != last+1 {
			t.Fatalf("p_no %d arrived after %d: send order differs from numbering order", n, last)
		}
		last = n
	}
	if last != 31 {
		t.Errorf("last p_no = %d, want 31", last)
	}
}

func TestRequestRateStaysUnderLimit(t *testing.T) {
	for _, limit := range []int{1, 3, 10} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			clk := tt.NewAutoClock(tt.AtJST(2026, 10, 8, 9, 0))
			fb := tt.New(t, clk)
			c := newClient(t, fb, clk, limit)
			loginOK(t, c, fb)
			for range 25 {
				if err := call(c, tachibana.TargetRequest, tachibana.PriorityWatchQuote); err != nil {
					t.Fatal(err)
				}
			}
			var at []time.Time
			for _, r := range fb.Requests() {
				at = append(at, r.At)
			}
			for i := limit; i < len(at); i++ {
				if gap := at[i].Sub(at[i-limit]); gap < time.Second {
					t.Fatalf("%d requests within %v (limit %d/s) at index %d", limit+1, gap, limit, i)
				}
			}
		})
	}
}

func TestRateLimitIsClampedToTheBrokersDesignLimit(t *testing.T) {
	for in, want := range map[int]int{0: 1, -3: 1, 1: 1, 7: 7, 10: 10, 99: 10} {
		if got := tachibana.NewClient(tachibana.Config{BaseURL: "https://x/", RequestsPerSecond: in}).RequestsPerSecond(); got != want {
			t.Errorf("RequestsPerSecond %d → %d, want %d", in, got, want)
		}
	}
}

func TestCallWithoutSession(t *testing.T) {
	clk := tt.NewAutoClock(tt.AtJST(2026, 10, 8, 9, 0))
	fb := tt.New(t, clk)
	c := newClient(t, fb, clk, 10)
	if err := call(c, tachibana.TargetRequest, tachibana.PriorityWatchQuote); err != broker.ErrNoSession {
		t.Fatalf("err = %v, want ErrNoSession", err)
	}
	if len(fb.Requests()) != 0 {
		t.Error("a request was sent without a session")
	}
}

func TestSessionEndsAtTheNightlyClose(t *testing.T) {
	clk := tt.NewAutoClock(tt.AtJST(2026, 10, 8, 20, 0))
	fb := tt.New(t, clk)
	c := newClient(t, fb, clk, 10)
	loginOK(t, c, fb)
	clk.SetNow(tt.AtJST(2026, 10, 9, 3, 30))
	if err := call(c, tachibana.TargetRequest, tachibana.PriorityWatchQuote); err == nil {
		t.Fatal("a request after 03:30 must not be sent with yesterday's virtual URL")
	}
	for range 8 {
		_ = call(c, tachibana.TargetPrice, tachibana.PriorityWatchQuote)
	}
	if got := c.BoardFailures().ConsecutiveFailures(); got != 0 {
		t.Errorf("market_data_down streak = %d during the nightly close, want 0", got)
	}
}
