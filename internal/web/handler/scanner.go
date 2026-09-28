package handler

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
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
// item shape.
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
			Return1m:        c.Return1m,
			Return5m:        c.Return5m,
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
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")

	if c.GetHeader("HX-Request") == "true" {
		_ = organisms.ScannerTableFallback(candidates, asOf).Render(c.Request.Context(), c.Writer)
		return
	}
	_ = pages.ScannerPage(candidates, asOf).Render(c.Request.Context(), c.Writer)
}

// scannerUpdateMessage mirrors docs/api/endpoints.md §6's
// `{"type":"scanner_update","items":[...]}` message.
type scannerUpdateMessage struct {
	Type  string        `json:"type"`
	Items []scannerItem `json:"items"`
}

// WebSocket implements `/ws/scanner` (docs/api/endpoints.md §6): pushes a
// `scanner_update` message on every connect, then again every
// h.interval.Next() (15-30s by default) until the client disconnects.
func (h *ScannerHandler) WebSocket(c *gin.Context) {
	conn, err := websocket.Accept(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()

	ctx := c.Request.Context()
	for {
		candidates, _, err := h.source.Candidates(ctx)
		if err != nil {
			return
		}
		msg := scannerUpdateMessage{Type: "scanner_update", Items: toScannerItems(candidates)}
		data, err := json.Marshal(msg)
		if err != nil {
			return
		}
		if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
			return
		}

		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		case <-time.After(h.interval.Next()):
		}
	}
}
