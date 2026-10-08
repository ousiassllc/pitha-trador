package broker_test

import (
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

func fp(v float64) *float64 { return &v }

func TestSymbolInfo_PriceLimit(t *testing.T) {
	info := broker.SymbolInfo{UpperLimit: fp(2500), LowerLimit: fp(2000)}
	cases := []struct {
		price float64
		want  domain.PriceLimit
	}{
		{2500, domain.PriceLimitUp},
		{2000, domain.PriceLimitDown},
		{2499, domain.PriceLimitNone},
		{2001, domain.PriceLimitNone},
	}
	for _, c := range cases {
		if got := info.PriceLimit(c.price); got != c.want {
			t.Errorf("PriceLimit(%v) = %q, want %q", c.price, got, c.want)
		}
	}
	if got := (broker.SymbolInfo{}).PriceLimit(1); got != domain.PriceLimitNone {
		t.Errorf("unknown limits: PriceLimit = %q, want none", got)
	}
}

func TestQuote_HasPrice(t *testing.T) {
	for price, want := range map[float64]bool{0: false, -1: false, 1500: true, math.Inf(1): false} {
		if got := (broker.Quote{Price: price}).HasPrice(); got != want {
			t.Errorf("HasPrice(%v) = %v, want %v", price, got, want)
		}
	}
}
