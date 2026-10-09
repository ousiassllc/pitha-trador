package market_test

import (
	"context"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

func ok(clm string, extra map[string]any) map[string]any {
	m := map[string]any{"sCLMID": clm, "p_errno": "0", "sResultCode": "0"}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// setup returns a logged-in client on an auto clock at 10:00 JST (daytime).
func setup(t *testing.T) (*tachibana.Client, *tt.FakeBroker, *tt.AutoClock) {
	t.Helper()
	clk := tt.NewAutoClock(tt.AtJST(2026, 10, 8, 10, 0))
	fb := tt.New(t, clk)
	c := tachibana.NewClient(tachibana.Config{BaseURL: fb.BaseURL(), RequestsPerSecond: 10, HTTPClient: fb.Server().Client(), Clock: clk})
	if _, err := c.Login(context.Background(), tt.AuthID, fb.Key()); err != nil {
		t.Fatal(err)
	}
	return c, fb, clk
}
