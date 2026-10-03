package activity_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/activity"
)

type fakeActivitySource struct {
	snap       domain.ActivitySnapshot
	err        error
	gotQuery   activityfeed.Query
	messages   chan activityfeed.Message
	subscribed chan struct{}
	cancelled  chan struct{}
}

func newFakeActivitySource(snap domain.ActivitySnapshot) *fakeActivitySource {
	return &fakeActivitySource{
		snap:       snap,
		messages:   make(chan activityfeed.Message, 4),
		subscribed: make(chan struct{}),
		cancelled:  make(chan struct{}),
	}
}

func (f *fakeActivitySource) Snapshot(_ context.Context, q activityfeed.Query) (domain.ActivitySnapshot, error) {
	f.gotQuery = q
	return f.snap, f.err
}

func (f *fakeActivitySource) Subscribe() (<-chan activityfeed.Message, func()) {
	close(f.subscribed)
	return f.messages, func() { close(f.cancelled) }
}

func sampleActivitySnapshot() domain.ActivitySnapshot {
	latency := 820
	return domain.ActivitySnapshot{
		Queues: []domain.QueueStatus{{Queue: "jev-scout", Pending: 3, Running: 1, FailedRecent: 2}},
		Events: []domain.ActivityEvent{
			{Type: domain.ActivityTypeJevTrader, Timestamp: time.Date(2026, 9, 29, 1, 15, 0, 0, time.UTC), Symbol: "7203", Detail: "direction=LONG confidence=0.74", LatencyMs: &latency},
			{Type: domain.ActivityTypeKillSwitch, Timestamp: time.Date(2026, 9, 29, 1, 10, 0, 0, time.UTC), Detail: "reason=daily_loss_limit"},
		},
		AsOf: time.Date(2026, 9, 29, 1, 15, 3, 0, time.UTC),
	}
}

func newActivityAPI(t *testing.T, h *activity.ActivityHandler) humatest.TestAPI {
	t.Helper()
	_, api := humatest.New(t, huma.DefaultConfig("test", "0.0.0"))
	huma.Get(api, "/api/v1/activity", h.APIActivity)
	return api
}

func TestActivityHandler_APIActivity_ReturnsSnapshotShapeAndPassesFilters(t *testing.T) {
	source := newFakeActivitySource(sampleActivitySnapshot())
	api := newActivityAPI(t, activity.NewActivityHandler(source))

	resp := api.Get("/api/v1/activity?limit=50&queue=jev-scout&type=job")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", resp.Code, resp.Body.String())
	}
	if source.gotQuery != (activityfeed.Query{Limit: 50, Queue: "jev-scout", Type: "job"}) {
		t.Fatalf("query passed to source = %+v", source.gotQuery)
	}

	var got struct {
		Queues []struct {
			Queue        string `json:"queue"`
			Pending      int    `json:"pending"`
			Running      int    `json:"running"`
			FailedRecent int    `json:"failed_recent"`
		} `json:"queues"`
		Events []map[string]any `json:"events"`
		AsOf   string           `json:"as_of"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, resp.Body.String())
	}
	if len(got.Queues) != 1 || got.Queues[0].Queue != "jev-scout" || got.Queues[0].Pending != 3 || got.Queues[0].Running != 1 || got.Queues[0].FailedRecent != 2 {
		t.Fatalf("queues = %+v", got.Queues)
	}
	if len(got.Events) != 2 {
		t.Fatalf("events = %+v", got.Events)
	}
	trader, kill := got.Events[0], got.Events[1]
	if trader["type"] != "jev_trader" || trader["symbol"] != "7203" || trader["latency_ms"] != float64(820) || trader["timestamp"] != "2026-09-29T01:15:00Z" {
		t.Fatalf("trader event = %+v", trader)
	}
	if _, has := kill["symbol"]; has {
		t.Fatalf("kill_switch event has a symbol key: %+v", kill)
	}
	if _, has := kill["latency_ms"]; has {
		t.Fatalf("kill_switch event has a latency_ms key: %+v", kill)
	}
	if got.AsOf != "2026-09-29T01:15:03Z" {
		t.Fatalf("as_of = %q", got.AsOf)
	}
}

func TestActivityHandler_APIActivity_RejectsOutOfRangeLimitAndUnknownFilters(t *testing.T) {
	api := newActivityAPI(t, activity.NewActivityHandler(newFakeActivitySource(sampleActivitySnapshot())))

	for _, q := range []string{"limit=501", "limit=0", "type=bogus", "queue=bogus"} {
		if resp := api.Get("/api/v1/activity?" + q); resp.Code != http.StatusUnprocessableEntity {
			t.Fatalf("GET ?%s status = %d, want 422", q, resp.Code)
		}
	}
	// Omitted limit defaults to the API default (functional.md FR-ACT-3).
	source := newFakeActivitySource(sampleActivitySnapshot())
	api = newActivityAPI(t, activity.NewActivityHandler(source))
	if resp := api.Get("/api/v1/activity"); resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	if source.gotQuery.Limit != activityfeed.DefaultLimit {
		t.Fatalf("default limit = %d, want %d", source.gotQuery.Limit, activityfeed.DefaultLimit)
	}
}

func TestActivityHandler_APIActivity_SourceErrorIs500(t *testing.T) {
	source := newFakeActivitySource(domain.ActivitySnapshot{})
	source.err = errors.New("db down")
	api := newActivityAPI(t, activity.NewActivityHandler(source))

	if resp := api.Get("/api/v1/activity"); resp.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.Code)
	}
}

func TestActivityHandler_Page_RendersIslandWithServerRenderedFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/activity", activity.NewActivityHandler(newFakeActivitySource(sampleActivitySnapshot())).Page)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/activity", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<pitha-activity-feed",
		`api-url="/api/v1/activity"`,
		`ws-url="/ws/activity"`,
		`kill-switch-events-url="/api/v1/activity?type=kill_switch&limit=10"`,
		"/static/dist/js/activity-feed/pitha-activity-feed.js",
		`data-queue="jev-scout"`,
		`data-event-type="jev_trader"`,
		"direction=LONG confidence=0.74",
		"reason=daily_loss_limit",
		"820",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

func TestActivityHandler_Page_SourceErrorIs500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	source := newFakeActivitySource(domain.ActivitySnapshot{})
	source.err = errors.New("db down")
	engine := gin.New()
	engine.GET("/activity", activity.NewActivityHandler(source).Page)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/activity", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `data-testid="error-page"`) || strings.Contains(rec.Body.String(), "db down") {
		t.Fatalf("500 should render the error page without the raw error, got %q", rec.Body.String())
	}
}

func TestActivityHandler_WebSocket_ForwardsBusMessagesAndUnsubscribesOnDisconnect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	source := newFakeActivitySource(domain.ActivitySnapshot{})
	engine := gin.New()
	engine.GET("/ws/activity", activity.NewActivityHandler(source).WebSocket)
	server := httptest.NewServer(engine)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/activity", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	select {
	case <-source.subscribed:
	case <-ctx.Done():
		t.Fatal("handler never subscribed")
	}

	source.messages <- activityfeed.Message{QueueUpdate: &activityfeed.QueueUpdate{Queue: "jev-scout", Pending: 2, Running: 1, FailedRecent: 4}}
	source.messages <- activityfeed.Message{Event: &domain.ActivityEvent{
		Type: domain.ActivityTypeJevScout, Timestamp: time.Date(2026, 9, 29, 1, 15, 0, 0, time.UTC), Symbol: "7203",
	}}

	read := func() map[string]any {
		t.Helper()
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("decode %s: %v", data, err)
		}
		return m
	}

	update := read()
	if update["type"] != "job_update" || update["queue"] != "jev-scout" || update["pending"] != float64(2) || update["running"] != float64(1) || update["failed_recent"] != float64(4) {
		t.Fatalf("job_update = %+v", update)
	}
	ev := read()
	inner, _ := ev["event"].(map[string]any)
	if ev["type"] != "activity_event" || inner["type"] != "jev_scout" || inner["symbol"] != "7203" || inner["timestamp"] != "2026-09-29T01:15:00Z" {
		t.Fatalf("activity_event = %+v", ev)
	}

	_ = conn.Close(websocket.StatusNormalClosure, "")
	select {
	case <-source.cancelled:
	case <-ctx.Done():
		t.Fatal("handler did not unsubscribe after client disconnect")
	}
}

func TestStaticActivitySource_ReportsAllQueuesIdle(t *testing.T) {
	snap, err := activity.StaticActivitySource{}.Snapshot(context.Background(), activityfeed.Query{})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Queues) != len(activityfeed.Queues()) || len(snap.Events) != 0 {
		t.Fatalf("snapshot = %+v, want every queue present and no events", snap)
	}
}
