package system_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
)

type fakeDailyHalt struct {
	halt *domain.DailyEntryHalt
	err  error
}

func (f fakeDailyHalt) DailyHalt(context.Context) (*domain.DailyEntryHalt, error) {
	return f.halt, f.err
}

// issue #753: GET /system/daily-halt shows why new entries are stopped, and
// is empty while they are not.
func TestDailyHaltHandler_Status(t *testing.T) {
	halt := &domain.DailyEntryHalt{
		Day: "2026-10-14", Reason: domain.DailyHaltReasonProfitLock,
		Message: "当日損益 +7000円 が最高 +10000円 から 30% 以上戻したため、利益を確保します。", HaltedAt: time.Now(),
	}
	tests := []struct {
		name     string
		source   system.DailyHaltSource
		want     []string
		wantNone bool
	}{
		{name: "halt in force", source: fakeDailyHalt{halt: halt},
			want: []string{`data-testid="daily-halt-banner"`, `data-reason="profit_lock"`, "利益確保（profit lock）", "+10000円", `href="/settings/risk"`}},
		{name: "no halt", source: fakeDailyHalt{}, wantNone: true},
		{name: "nil source", source: nil, wantNone: true},
		{name: "read failure shows nothing", source: fakeDailyHalt{err: errors.New("db down")}, wantNone: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			engine := gin.New()
			engine.GET("/system/daily-halt", system.NewDailyHaltHandler(tt.source).Status)
			rec := serve(engine, http.MethodGet, "/system/daily-halt")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			body := rec.Body.String()
			if tt.wantNone && strings.Contains(body, "daily-halt-banner") {
				t.Fatalf("rendered a banner: %s", body)
			}
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("body missing %q: %s", want, body)
				}
			}
		})
	}
}
