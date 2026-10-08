package broker_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

type fakeHealth struct{ board, api domain.FailureStreak }

func (f *fakeHealth) BoardFailures() *domain.FailureStreak  { return &f.board }
func (f *fakeHealth) BrokerFailures() *domain.FailureStreak { return &f.api }

func TestMarketDataChecker_StopsAfterConsecutiveBoardFailures(t *testing.T) {
	h := &fakeHealth{}
	c := broker.MarketDataChecker{Health: h}
	for i := 1; i <= broker.UnhealthyAfterBoardFailures; i++ {
		h.board.Fail()
		ok, err := c.Healthy(context.Background())
		if err != nil {
			t.Fatalf("Healthy: %v", err)
		}
		if want := i < broker.UnhealthyAfterBoardFailures; ok != want {
			t.Fatalf("Healthy = %v after %d failures, want %v", ok, i, want)
		}
	}
	h.board.Succeed()
	if ok, _ := c.Healthy(context.Background()); !ok {
		t.Error("Healthy = false after a success, want true")
	}
}

type codedErr struct{ code int }

func (e codedErr) Error() string   { return "coded" }
func (e codedErr) BrokerCode() int { return e.code }

type limitedErr struct{}

func (limitedErr) Error() string     { return "limited" }
func (limitedErr) RateLimited() bool { return true }

func TestErrorCodeAndIsRateLimited(t *testing.T) {
	if got := broker.ErrorCode(fmt.Errorf("wrapped: %w", codedErr{4001007})); got != 4001007 {
		t.Errorf("ErrorCode = %d, want 4001007", got)
	}
	if got := broker.ErrorCode(errors.New("plain")); got != 0 {
		t.Errorf("ErrorCode(plain) = %d, want 0", got)
	}
	for name, err := range map[string]error{
		"exhausted retry": fmt.Errorf("x: %w", broker.ErrRateLimited),
		"single reject":   fmt.Errorf("x: %w", limitedErr{}),
	} {
		if !broker.IsRateLimited(err) {
			t.Errorf("IsRateLimited(%s) = false", name)
		}
	}
	if broker.IsRateLimited(errors.New("plain")) || broker.IsRateLimited(codedErr{1}) {
		t.Error("IsRateLimited true for a non-rate-limit error")
	}
}
