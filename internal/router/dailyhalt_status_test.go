package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/router"
)

type fixedDailyHalt struct{ halt *domain.DailyEntryHalt }

func (f fixedDailyHalt) DailyHalt(context.Context) (*domain.DailyEntryHalt, error) {
	return f.halt, nil
}

// issue #753: the header banner route is wired through router.New.
func TestNew_DailyHaltRouteShowsTheHaltAndIsEmptyWithoutOne(t *testing.T) {
	gin.SetMode(gin.TestMode)
	halted := &domain.DailyEntryHalt{Day: "2026-10-14", Reason: domain.DailyHaltReasonMaxTrades, Message: "当日の取引回数 5回 が上限 5回 に達しました。"}
	for name, tc := range map[string]struct {
		opts []router.Option
		want string
	}{
		"halt in force":    {[]router.Option{router.WithDailyHaltSource(fixedDailyHalt{halt: halted})}, "1日の最大取引回数"},
		"no source wired":  {nil, ""},
		"no halt in force": {[]router.Option{router.WithDailyHaltSource(fixedDailyHalt{})}, ""},
	} {
		engine := router.New(tc.opts...)
		req := authorize(t, engine, httptest.NewRequest(http.MethodGet, "/system/daily-halt", nil))
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body=%s", name, rec.Code, rec.Body.String())
		}
		if got := strings.Contains(rec.Body.String(), tc.want); tc.want != "" && !got {
			t.Errorf("%s: body missing %q: %s", name, tc.want, rec.Body.String())
		}
		if tc.want == "" && strings.Contains(rec.Body.String(), "daily-halt-banner") {
			t.Errorf("%s: rendered a banner: %s", name, rec.Body.String())
		}
	}
}
