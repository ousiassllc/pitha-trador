package watchlist_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/watchlist"
)

type fakeSource struct {
	view domain.WatchListView
	err  error
}

func (f fakeSource) WatchLists(context.Context) (domain.WatchListView, error) { return f.view, f.err }

func get(t *testing.T, source watchlist.Source) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/watchlist", watchlist.NewHandler(source).Page)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/watchlist", nil))
	return rec.Code, rec.Body.String()
}

func TestPage_DisabledAndEmptyStates(t *testing.T) {
	if _, body := get(t, watchlist.StaticSource{}); !strings.Contains(body, `data-testid="watchlist-disabled"`) {
		t.Errorf("the idle source must say the list is not maintained: %s", body)
	}
	if _, body := get(t, fakeSource{view: domain.WatchListView{Enabled: true}}); !strings.Contains(body, `data-testid="watchlist-empty"`) {
		t.Errorf("an enabled screen without lists must say none is decided yet: %s", body)
	}
}

func TestPage_ShowsTheListsWithTheirOriginsAndIndicators(t *testing.T) {
	decided := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	view := domain.WatchListView{Enabled: true, ActiveDate: "2026-10-09", Lists: []domain.WatchList{
		{ListDate: "2026-10-09", Source: domain.WatchListDailyScreen, BasisDate: "2026-10-08", Reason: "日足スクリーニング（基準日 2026-10-08）", DecidedAt: decided,
			Entries: []domain.WatchListEntry{
				{Symbol: "7203", Origin: domain.WatchOriginHeld},
				{Symbol: "6758", Origin: domain.WatchOriginScreen, Indicators: []string{"gain_rate", "volume_surge"}},
			}},
		{ListDate: "2026-10-08", Source: domain.WatchListCarriedOver, Reason: "引き継ぎ", DecidedAt: decided},
	}}
	code, body := get(t, fakeSource{view: view})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	for _, want := range []string{
		`data-list-date="2026-10-09" data-source="daily_screen" data-active="true"`, `data-testid="watchlist-active"`,
		`data-symbol="7203" data-origin="held"`, `data-symbol="6758" data-origin="screen"`,
		"値上がり率・出来高急増", "保有・注文中", "基準日（日足）",
		`data-list-date="2026-10-08" data-source="carried_over" data-active="false"`, `data-testid="watchlist-fallback"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestPage_SourceErrorIs500WithoutLeakingIt(t *testing.T) {
	code, body := get(t, fakeSource{err: errors.New("db dsn=leakmarker")})
	if code != http.StatusInternalServerError || strings.Contains(body, "leakmarker") {
		t.Fatalf("status = %d body = %s", code, body)
	}
}
