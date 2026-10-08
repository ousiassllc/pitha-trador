package tachibana_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		name    string
		body    map[string]any
		kind    tachibana.ErrorKind
		code    int
		feed    bool // counts for market_data_down
		limited bool
	}{
		{"session expired", tt.ControlError("2", "session"), tachibana.KindSessionExpired, 2, true, false},
		{"out of hours", tt.ControlError("-62", "closed"), tachibana.KindOutOfHours, -62, false, false},
		{"busy -2", tt.ControlError("-2", "busy"), tachibana.KindBusy, -2, true, true},
		{"busy -3", tt.ControlError("-3", "busy"), tachibana.KindBusy, -3, true, true},
		{"halted 9", tt.ControlError("9", "stop"), tachibana.KindHalted, 9, true, false},
		{"halted -12", tt.ControlError("-12", "stop"), tachibana.KindHalted, -12, true, false},
		{"bad argument", tt.ControlError("-1", "arg"), tachibana.KindBadArgument, -1, false, false},
		{"p_no", tt.ControlError("6", "no"), tachibana.KindSequence, 6, true, false},
		{"clock", tt.ControlError("8", "time"), tachibana.KindClock, 8, true, false},
		{"business", map[string]any{"sCLMID": "X", "p_errno": "0", "sResultCode": "11111", "sResultText": "bad"}, tachibana.KindBusiness, 11111, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clk := tt.NewAutoClock(tt.AtJST(2026, 10, 8, 9, 0))
			fb := tt.New(t, clk)
			c := newClient(t, fb, clk, 10)
			loginOK(t, c, fb)
			fb.Respond(func(tt.Request, int) (int, map[string]any) { return 200, tc.body })

			err := call(c, tachibana.TargetPrice, tachibana.PriorityWatchQuote)
			var apiErr *tachibana.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if apiErr.Kind() != tc.kind || apiErr.BrokerCode() != tc.code || broker.ErrorCode(err) != tc.code {
				t.Errorf("kind=%v code=%d, want kind=%v code=%d", apiErr.Kind(), apiErr.BrokerCode(), tc.kind, tc.code)
			}
			if got := broker.IsRateLimited(err); got != tc.limited {
				t.Errorf("IsRateLimited = %v, want %v", got, tc.limited)
			}
			wantStreak := 0
			if tc.feed {
				wantStreak = 1
			}
			if got := c.BoardFailures().ConsecutiveFailures(); got != wantStreak {
				t.Errorf("market_data_down streak = %d, want %d", got, wantStreak)
			}
			if got := c.BrokerFailures().ConsecutiveFailures(); got != 0 {
				t.Errorf("broker_api_error streak = %d, want 0 (no HTTP 5xx)", got)
			}
		})
	}
}

func TestHTTPFailuresFeedTheStreaks(t *testing.T) {
	clk := tt.NewAutoClock(tt.AtJST(2026, 10, 8, 9, 0))
	fb := tt.New(t, clk)
	c := newClient(t, fb, clk, 10)
	loginOK(t, c, fb)
	status := http.StatusBadGateway
	fb.Respond(func(tt.Request, int) (int, map[string]any) { return status, map[string]any{} })

	for i := 1; i <= 3; i++ {
		err := call(c, tachibana.TargetPrice, tachibana.PriorityWatchQuote)
		var httpErr *tachibana.HTTPStatusError
		if !errors.As(err, &httpErr) || httpErr.Status != 502 {
			t.Fatalf("err = %v", err)
		}
		if c.BoardFailures().ConsecutiveFailures() != i || c.BrokerFailures().ConsecutiveFailures() != i {
			t.Fatalf("streaks = %d/%d after %d 5xx", c.BoardFailures().ConsecutiveFailures(), c.BrokerFailures().ConsecutiveFailures(), i)
		}
	}
	status = http.StatusNotFound // a 4xx is a feed failure but neither extends nor resets broker_api_error (as kabu)
	_ = call(c, tachibana.TargetPrice, tachibana.PriorityWatchQuote)
	if c.BoardFailures().ConsecutiveFailures() != 4 || c.BrokerFailures().ConsecutiveFailures() != 3 {
		t.Errorf("streaks after 404 = %d/%d, want 4/3", c.BoardFailures().ConsecutiveFailures(), c.BrokerFailures().ConsecutiveFailures())
	}
	fb.Respond(nil)
	if err := call(c, tachibana.TargetPrice, tachibana.PriorityWatchQuote); err != nil {
		t.Fatal(err)
	}
	if c.BoardFailures().ConsecutiveFailures() != 0 || c.BrokerFailures().ConsecutiveFailures() != 0 {
		t.Error("a success must reset both streaks")
	}
}

func TestClosedHoursAndNoDataDoNotCountAsFeedFailures(t *testing.T) {
	for _, err := range []error{
		&tachibana.APIError{Errno: tachibana.ErrnoOutOfHours}, &tachibana.APIError{Errno: tachibana.ErrnoBadArgument}, tachibana.ErrNoData,
		fmt.Errorf("wrapped: %w", tachibana.ErrNoData), context.Canceled, nil,
	} {
		if tachibana.CountsAsFeedFailure(err) {
			t.Errorf("%v counted as a feed failure", err)
		}
	}
	for _, err := range []error{
		&tachibana.APIError{Errno: tachibana.ErrnoBusy}, &tachibana.APIError{Errno: tachibana.ErrnoBusyOverload}, &tachibana.APIError{Errno: tachibana.ErrnoHalted},
		&tachibana.APIError{Errno: tachibana.ErrnoHaltedService}, &tachibana.APIError{Errno: tachibana.ErrnoSessionExpired},
		&tachibana.HTTPStatusError{Status: 503}, errors.New("dial"),
	} {
		if !tachibana.CountsAsFeedFailure(err) {
			t.Errorf("%v not counted as a feed failure", err)
		}
	}
}

func TestEveryDocumentedErrnoHasAKind(t *testing.T) {
	for _, n := range []int{-1, -2, -3, 2, 6, 8, 9, -12, -62} {
		if (&tachibana.APIError{Errno: n}).Kind() == tachibana.KindOther {
			t.Errorf("p_errno %d has no kind", n)
		}
	}
}

func TestShiftJISRoundTrip(t *testing.T) {
	clk := tt.NewAutoClock(tt.AtJST(2026, 10, 8, 9, 0))
	fb := tt.New(t, clk)
	c := newClient(t, fb, clk, 10)
	loginOK(t, c, fb)
	fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		return 200, map[string]any{"sCLMID": "X", "p_errno": "0", "sResultCode": "0", "sIssueName": "極 洋", "echo": req.Body["name"]}
	})
	var out struct {
		Name string `json:"sIssueName"`
		Echo string `json:"echo"`
	}
	if err := c.Call(context.Background(), tachibana.TargetMaster, tachibana.PriorityMaster, "CLMTest", map[string]string{"name": "ｶﾞﾗｽ土石製品"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "極 洋" || out.Echo != "ｶﾞﾗｽ土石製品" {
		t.Errorf("decoded %+v: Shift-JIS text was garbled", out)
	}
}

func TestResponseSizeIsCapped(t *testing.T) {
	clk := tt.NewAutoClock(tt.AtJST(2026, 10, 8, 9, 0))
	fb := tt.New(t, clk)
	c := newClient(t, fb, clk, 10)
	loginOK(t, c, fb)
	huge := strings.Repeat("a", 5<<20)
	fb.Respond(func(tt.Request, int) (int, map[string]any) {
		return 200, map[string]any{"sCLMID": "X", "p_errno": "0", "sResultCode": "0", "big": huge}
	})
	if err := call(c, tachibana.TargetPrice, tachibana.PriorityWatchQuote); err == nil {
		t.Fatal("a response over the cap must fail")
	}
	// The MASTER URL allows more (the 銘柄マスタ is large).
	if err := call(c, tachibana.TargetMaster, tachibana.PriorityMaster); err != nil {
		t.Fatalf("master within its cap: %v", err)
	}
}
