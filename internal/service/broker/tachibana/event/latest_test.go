package event_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/event"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

func TestLatestRefillsOverRESTAtMostOncePerInterval(t *testing.T) {
	r := newRig(t, 10, 60)
	_ = r.feed.SetWatch(r.ctx, []string{"7203", "6758", "9984"})
	var refills [][]string
	r.fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] != "CLMMfdsGetMarketPrice" {
			return http.StatusOK, nil
		}
		codes := strings.Split(req.Body["sTargetIssueCode"], ",")
		refills = append(refills, codes)
		var out []map[string]string
		for _, c := range codes {
			out = append(out, map[string]string{"sIssueCode": c, "pDPP": "1000", "pQBP": "999", "pQAP": "1001"})
		}
		return http.StatusOK, map[string]any{"sCLMID": "CLMMfdsGetMarketPrice", "p_errno": "0", "sResultCode": "0", "aCLMMfdsMarketPrice": out}
	})

	// No EVENT value: the first Latest refills every stale watched symbol in one request.
	q, err := r.feed.Latest(context.Background(), "7203")
	if err != nil || q.Price != 1000 {
		t.Fatalf("Latest = %+v, %v", q, err)
	}
	if len(refills) != 1 || strings.Join(refills[0], ",") != "7203,6758,9984" {
		t.Fatalf("refills = %v, want one request for the three stale symbols", refills)
	}
	if q, err := r.feed.Latest(context.Background(), "9984"); err != nil || q.Price != 1000 {
		t.Errorf("9984 right after the refill = %+v, %v", q, err)
	}

	// 31 s later the values are stale (> 30 s) but the interval (60 s) has not passed.
	r.clk.Advance(31 * time.Second)
	_, err = r.feed.Latest(context.Background(), "7203")
	if !errors.Is(err, event.ErrStale) || !errors.Is(err, broker.ErrPriceUnavailable) {
		t.Fatalf("err = %v, want ErrStale (also ErrPriceUnavailable)", err)
	}
	if len(refills) != 1 {
		t.Fatalf("refills = %d, want no second request inside the interval", len(refills))
	}

	// Past the interval one more refill is allowed.
	r.clk.Advance(30 * time.Second)
	if _, err := r.feed.Latest(context.Background(), "7203"); err != nil {
		t.Fatal(err)
	}
	if len(refills) != 2 {
		t.Errorf("refills = %d, want 2", len(refills))
	}
}

func TestLatestRefillIsOneRequestOfAtMost120SymbolsAndIncludesAnUnwatchedSymbol(t *testing.T) {
	r := newRig(t, 10, 60)
	var watch []string
	for i := 0; i < 130; i++ {
		watch = append(watch, fmt.Sprintf("%04d", i))
	}
	_ = r.feed.SetWatch(r.ctx, watch) // capped at 120
	var sizes []int
	r.fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] != "CLMMfdsGetMarketPrice" {
			return http.StatusOK, nil
		}
		codes := strings.Split(req.Body["sTargetIssueCode"], ",")
		sizes = append(sizes, len(codes))
		return http.StatusOK, map[string]any{"sCLMID": "CLMMfdsGetMarketPrice", "p_errno": "0", "sResultCode": "0",
			"aCLMMfdsMarketPrice": []map[string]string{{"sIssueCode": "7203", "pDPP": "3000"}}}
	})
	if _, err := r.feed.Latest(context.Background(), "7203"); err != nil { // not watched: refilled anyway
		t.Fatal(err)
	}
	if len(sizes) != 1 || sizes[0] != 120 {
		t.Errorf("request sizes = %v, want one request of 120 symbols (the requested one first)", sizes)
	}
}

func TestLatestSymbolWithoutDataIsErrNoData(t *testing.T) {
	r := newRig(t, 10, 60)
	r.fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] != "CLMMfdsGetMarketPrice" {
			return http.StatusOK, nil
		}
		return http.StatusOK, map[string]any{"sCLMID": "CLMMfdsGetMarketPrice", "p_errno": "0", "sResultCode": "0", "aCLMMfdsMarketPrice": []map[string]string{}}
	})
	if _, err := r.feed.Latest(context.Background(), "0000"); !errors.Is(err, tachibana.ErrNoData) {
		t.Errorf("err = %v, want ErrNoData", err)
	}
	if r.c.BoardFailures().ConsecutiveFailures() != 0 {
		t.Error("a symbol without data must not extend market_data_down")
	}
}

// RefillRequests widens one refill to that many 時価 requests of 120 symbols.
func TestLatestRefillMayUseSeveralRequestsPerRound(t *testing.T) {
	r := newRig(t, 10, 60)
	feed := event.New(event.Config{Client: r.c, RefillInterval: 60 * time.Second, RefillRequests: 2, HTTPClient: r.fb.Server().Client(), Clock: r.clk})
	var watch []string
	for i := 0; i < 120; i++ {
		watch = append(watch, fmt.Sprintf("%04d", i))
	}
	_ = feed.SetWatch(r.ctx, watch)
	var sizes []int
	r.fb.Respond(func(req tt.Request, _ int) (int, map[string]any) {
		if req.Body["sCLMID"] != "CLMMfdsGetMarketPrice" {
			return http.StatusOK, nil
		}
		codes := strings.Split(req.Body["sTargetIssueCode"], ",")
		sizes = append(sizes, len(codes))
		var out []map[string]string
		for _, c := range codes {
			out = append(out, map[string]string{"sIssueCode": c, "pDPP": "1000"})
		}
		return http.StatusOK, map[string]any{"sCLMID": "CLMMfdsGetMarketPrice", "p_errno": "0", "sResultCode": "0", "aCLMMfdsMarketPrice": out}
	})
	if _, err := feed.Latest(context.Background(), "9999"); err != nil { // 1 + 120 stale symbols = 121 → two requests
		t.Fatal(err)
	}
	if fmt.Sprint(sizes) != "[120 1]" {
		t.Errorf("request sizes = %v, want [120 1] (two requests in one round)", sizes)
	}
}
