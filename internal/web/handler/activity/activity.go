package activity

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// ActivitySource supplies System Activity Log's snapshot and live event
// stream (functional.md §4.15) for the routes below.
// *internal/service/activityfeed.Service implements it directly.
type ActivitySource interface {
	Snapshot(ctx context.Context, q activityfeed.Query) (domain.ActivitySnapshot, error)
	// Subscribe returns a stream of new-event messages and a cancel func
	// that must be called exactly once when the caller is done.
	Subscribe() (messages <-chan activityfeed.Message, cancel func())
}

// StaticActivitySource is an idle ActivitySource (every queue empty, no
// events, no live messages), used as internal/router.New()'s default
// until a real activityfeed.Service is wired in.
type StaticActivitySource struct{}

func (StaticActivitySource) Snapshot(context.Context, activityfeed.Query) (domain.ActivitySnapshot, error) {
	queues := make([]domain.QueueStatus, 0, len(activityfeed.Queues()))
	for _, name := range activityfeed.Queues() {
		queues = append(queues, domain.QueueStatus{Queue: name})
	}
	return domain.ActivitySnapshot{Queues: queues, AsOf: time.Now().UTC()}, nil
}

func (StaticActivitySource) Subscribe() (<-chan activityfeed.Message, func()) {
	return make(chan activityfeed.Message), func() {}
}

// ActivityHandler implements System Activity Log's routes
// (docs/api/endpoints.md §3, §5, §6): `GET /api/v1/activity`,
// `GET /activity`, and `/ws/activity`.
type ActivityHandler struct {
	source ActivitySource
}

// NewActivityHandler returns an ActivityHandler backed by source.
func NewActivityHandler(source ActivitySource) *ActivityHandler {
	return &ActivityHandler{source: source}
}

// activityQueueOutput mirrors docs/api/endpoints.md §5 `GET
// /api/v1/activity`'s `queues[]` item shape.
type activityQueueOutput struct {
	Queue        string `json:"queue"`
	Pending      int    `json:"pending"`
	Running      int    `json:"running"`
	FailedRecent int    `json:"failed_recent"`
}

// activityEventOutput mirrors docs/api/endpoints.md §5 `GET
// /api/v1/activity`'s `events[]` item shape, also embedded in
// `/ws/activity` `activity_event` messages.
type activityEventOutput struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Queue     string    `json:"queue,omitempty" doc:"jobs.queue; set only for type=job."`
	Symbol    string    `json:"symbol,omitempty"`
	Detail    string    `json:"detail"`
	LatencyMs *int      `json:"latency_ms,omitempty"`
}

func toActivityEventOutput(e domain.ActivityEvent) activityEventOutput {
	return activityEventOutput{
		Type: e.Type, Timestamp: e.Timestamp, Queue: e.Queue, Symbol: e.Symbol,
		Detail: e.Detail, LatencyMs: e.LatencyMs,
	}
}

// ActivityAPIInput is `GET /api/v1/activity`'s query.
type ActivityAPIInput struct {
	Limit int    `query:"limit" default:"200" minimum:"1" maximum:"500" doc:"Feed size (default 200, max 500)."`
	Queue string `query:"queue" enum:"market-data,feature-calc,jev-scout,jev-trader,outcome-labeling,analytics" doc:"Only job events on this jobs.queue."`
	Type  string `query:"type" enum:"job,jev_scout,jev_trader,kill_switch" doc:"Only events of this type."`
}

// ActivityAPIOutput is the Huma response body for `GET /api/v1/activity`.
type ActivityAPIOutput struct {
	Body struct {
		Queues []activityQueueOutput `json:"queues"`
		Events []activityEventOutput `json:"events"`
		AsOf   time.Time             `json:"as_of"`
	}
}

// APIActivity implements `GET /api/v1/activity` (docs/api/endpoints.md
// §5): per-queue depth plus the merged, newest-first activity feed.
func (h *ActivityHandler) APIActivity(ctx context.Context, in *ActivityAPIInput) (*ActivityAPIOutput, error) {
	snap, err := h.source.Snapshot(ctx, activityfeed.Query{Limit: in.Limit, Queue: in.Queue, Type: in.Type})
	if err != nil {
		return nil, huma.Error500InternalServerError("load activity failed", err)
	}

	out := &ActivityAPIOutput{}
	out.Body.Queues = make([]activityQueueOutput, len(snap.Queues))
	for i, q := range snap.Queues {
		out.Body.Queues[i] = activityQueueOutput{Queue: q.Queue, Pending: q.Pending, Running: q.Running, FailedRecent: q.FailedRecent}
	}
	out.Body.Events = make([]activityEventOutput, len(snap.Events))
	for i, e := range snap.Events {
		out.Body.Events[i] = toActivityEventOutput(e)
	}
	out.Body.AsOf = snap.AsOf
	return out, nil
}

// Page implements `GET /activity` (docs/api/endpoints.md §3): the System
// Activity Log page, SSR-rendered from the current snapshot and hydrated
// by `pitha-activity-feed`. Like CalibrationHandler.Page there is no
// HX-Request fragment variant; live updates come over `/ws/activity`.
func (h *ActivityHandler) Page(c *gin.Context) {
	snap, err := h.source.Snapshot(c.Request.Context(), activityfeed.Query{})
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "handler: activity page snapshot", "error", err)
		shared.RespondPageError(c, http.StatusInternalServerError, "アクティビティログの取得に失敗しました。")
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = pages.ActivityLogPage(snap).Render(c.Request.Context(), c.Writer)
}

// activityJobUpdateMessage mirrors docs/api/endpoints.md §6's
// `{"type":"job_update","queue":...,"pending":...,"running":...}`
// message (plus `failed_recent`).
type activityJobUpdateMessage struct {
	Type         string `json:"type"`
	Queue        string `json:"queue"`
	Pending      int    `json:"pending"`
	Running      int    `json:"running"`
	FailedRecent int    `json:"failed_recent"`
}

// activityEventMessage mirrors docs/api/endpoints.md §6's
// `{"type":"activity_event","event":{...}}` message.
type activityEventMessage struct {
	Type  string              `json:"type"`
	Event activityEventOutput `json:"event"`
}

// WebSocket implements `/ws/activity` (docs/api/endpoints.md §6): from
// connect until the client disconnects, forwards each new activity event
// and queue-depth change as it happens. It sends nothing on connect - the
// client takes its initial state from `GET /api/v1/activity` (functional.md
// FR-ACT-4).
func (h *ActivityHandler) WebSocket(c *gin.Context) {
	conn, err := shared.AcceptWebSocket(c)
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()

	// The client never sends; CloseRead drains/handles control frames and
	// cancels ctx when the client goes away, so the loop below exits
	// instead of blocking on messages that will never arrive.
	ctx := conn.CloseRead(c.Request.Context())

	messages, cancel := h.source.Subscribe()
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		case msg, ok := <-messages:
			if !ok {
				return
			}
			if err := shared.WriteJSON(ctx, conn, activityWSPayload(msg)); err != nil {
				return
			}
		}
	}
}

func activityWSPayload(msg activityfeed.Message) any {
	if msg.QueueUpdate != nil {
		u := msg.QueueUpdate
		return activityJobUpdateMessage{
			Type: "job_update", Queue: u.Queue, Pending: u.Pending, Running: u.Running, FailedRecent: u.FailedRecent,
		}
	}
	return activityEventMessage{Type: "activity_event", Event: toActivityEventOutput(*msg.Event)}
}
