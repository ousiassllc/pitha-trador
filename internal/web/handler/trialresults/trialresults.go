package trialresults

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/trialquery"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/trialresults"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// Source reads the trial's aggregation over a period: jev_trial_compare's and
// jev_trial_cost's data, for every arm. internal/bootstrap/trialview's Reader
// implements it over the application's database in a read-only transaction.
type Source interface {
	Compare(ctx context.Context, r query.Range) (trialquery.Comparison, error)
	Cost(ctx context.Context, r query.Range, now time.Time) (trialquery.CostReport, error)
}

// StaticSource is internal/router.New()'s default Source (router-level tests
// only): no trial record exists.
type StaticSource struct{}

func (StaticSource) Compare(context.Context, query.Range) (trialquery.Comparison, error) {
	return trialquery.Comparison{}, nil
}

func (StaticSource) Cost(context.Context, query.Range, time.Time) (trialquery.CostReport, error) {
	return trialquery.CostReport{}, nil
}

// Periods the page offers (JST days up to and including today), and the one
// shown without a `days` query parameter (the MCP tools' default too).
var periodDays = []int{7, 30, 90}

const defaultDays = 30

// BasePath is the お試し tab's URL.
const BasePath = pages.TrialTabHref

// Handler implements `GET /performance/trial`.
type Handler struct {
	source Source
	now    func() time.Time
}

// New returns a Handler reading from source.
func New(source Source) *Handler { return &Handler{source: source, now: time.Now} }

// Register mounts the route on engine. It is GET only.
func (h *Handler) Register(engine *gin.Engine) { engine.GET(BasePath, h.Page) }

// Page implements `GET /performance/trial`: the last 7, 30 (default) or 90 JST
// days (`days`) of the trial's missing rate, skips, cost by source and
// NONE / counterfactual proxy figures. A read failure is a 500 with a fixed
// message in place of the figures.
func (h *Handler) Page(c *gin.Context) {
	now := h.now()
	days := daysOf(c.Query("days"))
	props := trialresults.Props{Periods: periodLinks(days)}
	status := http.StatusOK

	r := periodRange(now, days)
	ctx := c.Request.Context()
	comparison, err := h.source.Compare(ctx, r)
	var cost trialquery.CostReport
	if err == nil {
		cost, err = h.source.Cost(ctx, r, now)
	}
	if err != nil {
		slog.ErrorContext(ctx, "handler: trial results", "error", err)
		status, props.Error = http.StatusInternalServerError, "お試しの成績を読み込めませんでした。"
		if errors.Is(err, query.ErrLimitExceeded) {
			props.Error = "記録が多すぎて集計できません。期間を短くしてください。"
		}
	} else {
		fill(&props, comparison, cost)
	}
	shared.RenderHTML(c, status, pages.TrialResultsPage(props))
}

// daysOf is the period of the `days` query value: one of periodDays, else the default.
func daysOf(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return defaultDays
	}
	for _, d := range periodDays {
		if d == n {
			return n
		}
	}
	return defaultDays
}

// periodRange is the last days JST days including today: the MCP's default
// period, [tomorrow 00:00 JST - days, tomorrow 00:00 JST).
func periodRange(now time.Time, days int) query.Range {
	y, m, d := now.In(query.JST).Date()
	tomorrow := time.Date(y, m, d, 0, 0, 0, 0, query.JST).AddDate(0, 0, 1)
	return query.Range{From: tomorrow.AddDate(0, 0, -days), To: tomorrow}
}

func periodLinks(current int) []trialresults.PeriodLink {
	links := make([]trialresults.PeriodLink, len(periodDays))
	for i, d := range periodDays {
		links[i] = trialresults.PeriodLink{Label: "直近 " + strconv.Itoa(d) + " 日", Href: BasePath + "?days=" + strconv.Itoa(d), Current: d == current}
	}
	return links
}
