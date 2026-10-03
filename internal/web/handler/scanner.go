package handler

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// CandidateSource supplies the Fast Screener-selected candidates
// (internal/service/screener) currently on display, for the Scanner
// API/SSR page/WebSocket routes below. A later sub-scope implements it on
// top of the Scheduler's live scan-cycle output; until then
// StaticCandidateSource is a fixed/in-memory stand-in.
type CandidateSource interface {
	// Candidates returns the current candidate list and the scan-cycle
	// timestamp it was computed at.
	Candidates(ctx context.Context) ([]domain.Candidate, time.Time, error)
}

// StaticCandidateSource is a fixed CandidateSource, used as
// internal/router.New()'s default until a real implementation is wired
// in.
type StaticCandidateSource struct {
	Items []domain.Candidate
	AsOf  time.Time
}

// Candidates returns s.Items/s.AsOf as-is; if AsOf is the zero value it
// returns the current time instead, so an unconfigured default source
// still reports a sensible freshness timestamp.
func (s StaticCandidateSource) Candidates(context.Context) ([]domain.Candidate, time.Time, error) {
	asOf := s.AsOf
	if asOf.IsZero() {
		asOf = time.Now()
	}
	return s.Items, asOf, nil
}

// CandidateRefreshInterval reports the range Scanner Dashboard/WebSocket
// candidate refreshes should be spaced within (functional.md §4.3,
// scan.candidate_refresh_interval_seconds_min/max in config/strategy.yaml).
type CandidateRefreshInterval struct {
	Min time.Duration
	Max time.Duration
}

// Next returns a random duration in [Min, Max] (functional.md §5.1
// "候補銘柄更新周期（15〜30秒）"), or Min if Max <= Min.
func (r CandidateRefreshInterval) Next() time.Duration {
	if r.Max <= r.Min {
		return r.Min
	}
	span := r.Max - r.Min
	return r.Min + time.Duration(rand.Int64N(int64(span)))
}

// ScannerHandler implements the Scanner Dashboard/API/WebSocket routes
// (docs/api/endpoints.md §3, §5, §6): `GET /api/v1/scanner`,
// `GET /scanner`, and `/ws/scanner`.
type ScannerHandler struct {
	source   CandidateSource
	interval CandidateRefreshInterval
}

// NewScannerHandler returns a ScannerHandler that reads candidates from
// source and pushes WebSocket updates spaced within interval.
func NewScannerHandler(source CandidateSource, interval CandidateRefreshInterval) *ScannerHandler {
	return &ScannerHandler{source: source, interval: interval}
}

// scannerItem mirrors docs/api/endpoints.md §5 `GET /api/v1/scanner`'s
// item shape. Return1m/Return5m are percent (0.4 == +0.4%), converted from
// the Feature Engine's decimal ratios by toScannerItems.
type scannerItem struct {
	Symbol          string   `json:"symbol"`
	Price           float64  `json:"price"`
	Return1m        *float64 `json:"return_1m"`
	Return5m        *float64 `json:"return_5m"`
	VolumeRatio5m   *float64 `json:"volume_ratio_5m"`
	PriceVsVWAPBps  float64  `json:"price_vs_vwap_bps"`
	SpreadBps       *float64 `json:"spread_bps"`
	JevDirection    *string  `json:"jev_direction"`
	JevConfidence   *float64 `json:"jev_confidence"`
	EntryQuality    *string  `json:"entry_quality"`
	CurrentPosition *float64 `json:"current_position"`
}

func toScannerItems(candidates []domain.Candidate) []scannerItem {
	items := make([]scannerItem, len(candidates))
	for i, c := range candidates {
		items[i] = scannerItem{
			Symbol:          c.Symbol,
			Price:           c.Price,
			Return1m:        domain.RatioToPercentPtr(c.Return1m),
			Return5m:        domain.RatioToPercentPtr(c.Return5m),
			VolumeRatio5m:   c.VolumeRatio5m,
			PriceVsVWAPBps:  c.PriceVsVWAPBps,
			SpreadBps:       c.SpreadBps,
			JevDirection:    c.JevDirection,
			JevConfidence:   c.JevConfidence,
			EntryQuality:    c.EntryQuality,
			CurrentPosition: c.CurrentPosition,
		}
	}
	return items
}

// ScannerAPIOutput is the Huma response body for `GET /api/v1/scanner`.
type ScannerAPIOutput struct {
	Body struct {
		Items []scannerItem `json:"items"`
		AsOf  time.Time     `json:"as_of"`
	}
}

// APIScanner implements `GET /api/v1/scanner` (docs/api/endpoints.md §5):
// the Fast Screener-selected, Jev-evaluated (once available) candidate
// list as JSON.
func (h *ScannerHandler) APIScanner(ctx context.Context, _ *struct{}) (*ScannerAPIOutput, error) {
	candidates, asOf, err := h.source.Candidates(ctx)
	if err != nil {
		return nil, err
	}
	out := &ScannerAPIOutput{}
	out.Body.Items = toScannerItems(candidates)
	out.Body.AsOf = asOf
	return out, nil
}

// Page implements `GET /scanner` (docs/api/endpoints.md §3): the full
// Scanner Dashboard page, or - when the request carries an `HX-Request`
// header - the candidate table fragment alone.
func (h *ScannerHandler) Page(c *gin.Context) {
	candidates, asOf, err := h.source.Candidates(c.Request.Context())
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "handler: scanner page candidates", "error", err)
		shared.RespondPageError(c, http.StatusInternalServerError, "候補一覧の取得に失敗しました。")
		return
	}

	if c.GetHeader("HX-Request") == "true" {
		shared.RenderHTML(c, http.StatusOK, organisms.ScannerTableFallback(candidates, asOf))
		return
	}
	// The scan panel (issue #303) starts closed: funnel only. A failure to
	// read it must not take the candidate list down with it.
	panel, err := h.scanPanel(c.Request.Context(), domain.ScanQuery{}, false)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "handler: scanner page scan summary", "error", err)
	}
	shared.RenderHTML(c, http.StatusOK, pages.ScannerPage(candidates, asOf, panel))
}

// scannerUpdateMessage mirrors docs/api/endpoints.md §6's
// `{"type":"scanner_update","items":[...],"as_of":"..."}` message. AsOf is
// the same scan-cycle timestamp (and RFC 3339 format/offset) as
// `GET /api/v1/scanner`'s `as_of`, so the table caption does not change
// timezone when the first push replaces the initial fetch.
type scannerUpdateMessage struct {
	Type  string        `json:"type"`
	Items []scannerItem `json:"items"`
	AsOf  time.Time     `json:"as_of"`
}

// WebSocket implements `/ws/scanner` (docs/api/endpoints.md §6): pushes a
// `scanner_update` message on every connect, then again every
// h.interval.Next() (15-30s by default) until the client disconnects.
func (h *ScannerHandler) WebSocket(c *gin.Context) {
	shared.PollWebSocket(c, h.interval.Next, func(ctx context.Context, conn *websocket.Conn) error {
		candidates, asOf, err := h.source.Candidates(ctx)
		if err != nil {
			return shared.Transient(err)
		}
		return shared.WriteJSON(ctx, conn, scannerUpdateMessage{Type: "scanner_update", Items: toScannerItems(candidates), AsOf: asOf})
	})
}
