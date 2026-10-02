package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// ScanSource supplies the latest scan cycle's per-symbol results (issue
// #303). A CandidateSource that also implements ScanSource
// (*internal/service/screener.LiveSource) enables the Scanner Dashboard's
// scan view; otherwise the view reports "no cycle yet". It is a separate
// interface so the candidate read path used by `GET /api/v1/scanner` and
// `/ws/scanner` is untouched.
type ScanSource interface {
	// Scan returns the latest cycle, or ok=false before the first cycle
	// has run. The cycle's Symbols slice is shared and must not be modified.
	Scan(ctx context.Context) (cycle domain.ScanCycle, ok bool, err error)
}

// scan returns the latest cycle, or ok=false if none (or no ScanSource).
func (h *ScannerHandler) scan(ctx context.Context) (domain.ScanCycle, bool, error) {
	src, ok := h.source.(ScanSource)
	if !ok {
		return domain.ScanCycle{}, false, nil
	}
	return src.Scan(ctx)
}

// scanReasonJSON is a reason code with its human-readable label.
type scanReasonJSON struct {
	Code  string `json:"code" doc:"Stable reason code, e.g. min_price, max_spread_bps, missing_spread."`
	Label string `json:"label" doc:"Human-readable Japanese label."`
}

type scanReasonCountJSON struct {
	scanReasonJSON
	Kind  string `json:"kind" enum:"threshold,missing" doc:"threshold = failed a Fast Screener condition; missing = the value could not be computed."`
	Count int    `json:"count" doc:"Symbols out for this reason in the latest cycle (a symbol out for several reasons counts under each)."`
}

type scanFunnelJSON struct {
	Universe           int `json:"universe" doc:"Active stock instruments scanned."`
	FeatureComputed    int `json:"feature_computed" doc:"Instruments with a computed snapshot/features."`
	FastScreenerPassed int `json:"fast_screener_passed" doc:"Fast Screener candidates (after top_n)."`
	ScoutEvaluated     int `json:"scout_evaluated" doc:"Candidates Jev Scout has judged so far in this cycle."`
	ScoutPassed        int `json:"scout_passed" doc:"Candidates that passed Jev Scout so far in this cycle."`
}

type scanSymbolJSON struct {
	Symbol  string           `json:"symbol"`
	Name    string           `json:"name"`
	Market  string           `json:"market"`
	Status  string           `json:"status" enum:"passed,excluded,missing"`
	Reasons []scanReasonJSON `json:"reasons" doc:"Why the symbol is not a candidate; empty when passed."`
	Scout   *string          `json:"scout" enum:"passed,failed,error" doc:"Jev Scout outcome for a candidate; null when not a candidate or still pending."`
}

// ScannerScanInput is the query of `GET /api/v1/scanner/scan`.
type ScannerScanInput struct {
	Q        string `query:"q" maxLength:"64" doc:"Case-insensitive substring of symbol or name."`
	Status   string `query:"status" enum:"passed,excluded,missing" doc:"Only symbols with this status."`
	Reason   string `query:"reason" maxLength:"32" doc:"Only symbols out for this reason code (see reason_counts)."`
	Page     int    `query:"page" default:"1" minimum:"1" doc:"1-based page."`
	PageSize int    `query:"page_size" default:"50" minimum:"1" maximum:"200" doc:"Rows per page (1-200)."`
}

// ScannerScanOutput is the Huma response of `GET /api/v1/scanner/scan`.
type ScannerScanOutput struct {
	Body struct {
		HasCycle     bool                  `json:"has_cycle" doc:"False until the first scan cycle has run; every other field is then empty."`
		StartedAt    *time.Time            `json:"started_at"`
		FinishedAt   *time.Time            `json:"finished_at"`
		DurationMs   int64                 `json:"duration_ms"`
		Funnel       scanFunnelJSON        `json:"funnel"`
		Passed       int                   `json:"passed"`
		Excluded     int                   `json:"excluded"`
		Missing      int                   `json:"missing"`
		ReasonCounts []scanReasonCountJSON `json:"reason_counts"`
		Total        int                   `json:"total" doc:"Symbols matching the filters, across all pages."`
		Page         int                   `json:"page"`
		PageSize     int                   `json:"page_size"`
		Pages        int                   `json:"pages"`
		Items        []scanSymbolJSON      `json:"items"`
	}
}

// APIScannerScan implements `GET /api/v1/scanner/scan` (docs/api/endpoints
// §5): the latest cycle's funnel counts, reason tallies and one filtered,
// paged window of per-symbol verdicts.
func (h *ScannerHandler) APIScannerScan(ctx context.Context, in *ScannerScanInput) (*ScannerScanOutput, error) {
	query := domain.ScanQuery{Q: in.Q, Status: domain.ScanStatus(in.Status), Page: in.Page, PageSize: in.PageSize}
	if in.Reason != "" {
		r, ok := domain.ScreenReasonFromCode(in.Reason)
		if !ok {
			return nil, huma.Error400BadRequest("unknown reason code " + strconv.Quote(in.Reason))
		}
		query.Reason = &r
	}
	cycle, ok, err := h.scan(ctx)
	if err != nil {
		return nil, err
	}
	out := &ScannerScanOutput{}
	b := &out.Body
	b.Items, b.ReasonCounts = []scanSymbolJSON{}, []scanReasonCountJSON{}
	b.Page, b.Pages = 1, 1
	if !ok {
		return out, nil
	}
	sum, page := cycle.Summary(), cycle.Query(query)
	b.HasCycle = true
	b.StartedAt, b.FinishedAt, b.DurationMs = &cycle.StartedAt, &cycle.FinishedAt, cycle.Duration().Milliseconds()
	b.Funnel = scanFunnelJSON(cycle.Funnel)
	b.Passed, b.Excluded, b.Missing = sum.Passed, sum.Excluded, sum.Missing
	for _, r := range domain.AllScreenReasons() {
		kind := "threshold"
		if r.IsMissing() {
			kind = "missing"
		}
		b.ReasonCounts = append(b.ReasonCounts, scanReasonCountJSON{scanReasonJSON{r.Code(), r.Label()}, kind, sum.ByReason[r]})
	}
	b.Total, b.Page, b.PageSize, b.Pages = page.Total, page.Page, page.PageSize, page.Pages
	for _, s := range page.Symbols {
		item := scanSymbolJSON{Symbol: s.Symbol, Name: s.Name, Market: s.Market, Status: string(s.Status()), Reasons: []scanReasonJSON{}}
		for _, r := range s.Reasons.List() {
			item.Reasons = append(item.Reasons, scanReasonJSON{r.Code(), r.Label()})
		}
		if o, ok := cycle.Scout[s.Symbol]; ok {
			v := string(o)
			item.Scout = &v
		}
		b.Items = append(b.Items, item)
	}
	return out, nil
}

// parseScanQuery reads the SSR scan view's query string. Unlike the JSON
// API it is lenient: an unknown status/reason or a non-numeric page is
// ignored rather than rejected, so a stale bookmark still renders.
func parseScanQuery(c *gin.Context) domain.ScanQuery {
	q := domain.ScanQuery{Q: c.Query("q")}
	switch s := domain.ScanStatus(c.Query("status")); s {
	case domain.ScanStatusPassed, domain.ScanStatusExcluded, domain.ScanStatusMissing:
		q.Status = s
	}
	if r, ok := domain.ScreenReasonFromCode(c.Query("reason")); ok {
		q.Reason = &r
	}
	q.Page, _ = strconv.Atoi(c.Query("page"))
	q.PageSize, _ = strconv.Atoi(c.Query("page_size"))
	return q
}

// ScanView implements `GET /scanner/scan` (docs/api/endpoints.md §3): the
// Scanner Dashboard's scan panel - funnel summary plus the filtered,
// paged symbol list - as an HTMX fragment for an HX-Request (the "scan
// targets" entry, filters, pager and refresh button), or inside the full
// page otherwise. `open=0` renders the funnel only. It reads the latest
// cycle once per request.
func (h *ScannerHandler) ScanView(c *gin.Context) {
	ctx := c.Request.Context()
	panel, err := h.scanPanel(ctx, parseScanQuery(c), c.Query("open") != "0")
	if err != nil {
		slog.ErrorContext(ctx, "handler: scanner scan view", "error", err)
		shared.RespondPageError(c, http.StatusInternalServerError, "スキャン結果の取得に失敗しました。")
		return
	}
	if c.GetHeader("HX-Request") == "true" {
		c.Status(http.StatusOK)
		c.Header("Content-Type", "text/html; charset=utf-8")
		_ = organisms.ScanPanel(panel).Render(ctx, c.Writer)
		return
	}
	candidates, asOf, err := h.source.Candidates(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "handler: scanner page candidates", "error", err)
		shared.RespondPageError(c, http.StatusInternalServerError, "候補一覧の取得に失敗しました。")
		return
	}
	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = pages.ScannerPage(candidates, asOf, panel).Render(ctx, c.Writer)
}

// scanPanel builds the scan panel's view model; open also loads the
// symbol list window for query. On error the returned view is the
// empty-state one.
func (h *ScannerHandler) scanPanel(ctx context.Context, query domain.ScanQuery, open bool) (organisms.ScanPanelView, error) {
	cycle, ok, err := h.scan(ctx)
	if err != nil || !ok {
		return organisms.ScanPanelView{Open: open, Query: query}, err
	}
	view := organisms.ScanPanelView{HasCycle: true, Cycle: cycle, Summary: cycle.Summary(), Open: open, Query: query}
	if open {
		view.Page = cycle.Query(query)
		view.Query.Page, view.Query.PageSize = view.Page.Page, view.Page.PageSize
	}
	return view, nil
}
