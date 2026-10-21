package orderproposals

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proposal"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

const (
	// pageSize is how many proposals one page of the list shows.
	pageSize = 50
	// defaultRangeDays is the list's default period: today and the 29 days
	// before it (JST), the MCP tools' default of 30 days.
	defaultRangeDays = 30
	dateLayout       = "2006-01-02"
)

// Handler implements `GET /order-proposals` and `GET /order-proposals/:id`.
type Handler struct {
	source Source
	now    func() time.Time
}

// NewHandler returns a Handler reading from source.
func NewHandler(source Source) *Handler { return &Handler{source: source, now: time.Now} }

// Register mounts the two routes on engine.
func (h *Handler) Register(engine *gin.Engine) {
	engine.GET("/order-proposals", h.List)
	engine.GET("/order-proposals/:id", h.Detail)
}

// List renders the list. Its query parameters are from / to (JST dates, to
// inclusive), status, bucket and cursor; a value that does not parse or is not
// a known code is a 400 with the reason above the form.
func (h *Handler) List(c *gin.Context) {
	props := pages.ProposalListProps{Statuses: statusOptions(), Buckets: bucketOptions()}
	filter, errs := h.parseFilter(c)
	props.Filter = pages.ProposalFilter{From: c.Query("from"), To: c.Query("to"), Status: filter.Status, Bucket: filter.Bucket}
	today := h.now().In(query.JST)
	if props.Filter.From == "" {
		props.Filter.From = today.AddDate(0, 0, 1-defaultRangeDays).Format(dateLayout)
	}
	if props.Filter.To == "" {
		props.Filter.To = today.Format(dateLayout)
	}
	if len(errs) > 0 {
		props.Errors = errs
		shared.RenderHTML(c, http.StatusBadRequest, pages.OrderProposalsPage(props))
		return
	}

	status := http.StatusOK
	items, next, err := h.source.List(c.Request.Context(), filter)
	switch {
	case errors.Is(err, query.ErrBadCursor):
		props.Errors, status = []string{"cursor が正しくありません。一覧の先頭から開き直してください。"}, http.StatusBadRequest
	case err != nil:
		slog.ErrorContext(c.Request.Context(), "handler: order proposals list", "error", err)
		props.LoadError, status = "注文案の取得に失敗しました。", http.StatusInternalServerError
	default:
		props.Rows = rows(items)
		if next != "" {
			props.NextHref = nextHref(props.Filter, next)
		}
	}
	shared.RenderHTML(c, status, pages.OrderProposalsPage(props))
}

// Detail renders one proposal, 404 for an unknown or malformed id.
func (h *Handler) Detail(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		shared.RespondPageError(c, http.StatusNotFound, "注文案が見つかりません。")
		return
	}
	state, err := h.source.Get(c.Request.Context(), id)
	if errors.Is(err, proposal.ErrNotFound) {
		shared.RespondPageError(c, http.StatusNotFound, "注文案が見つかりません。")
		return
	}
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "handler: order proposal detail", "proposal_id", id, "error", err)
		shared.RespondPageError(c, http.StatusInternalServerError, "注文案の取得に失敗しました。")
		return
	}
	shared.RenderHTML(c, http.StatusOK, pages.OrderProposalDetailPage(detail(state)))
}

// parseFilter reads the query parameters into the read model's filter over
// [from 00:00 JST, to+1 00:00 JST). The returned messages (Japanese, one per
// line) are empty when the input is valid.
func (h *Handler) parseFilter(c *gin.Context) (proposal.ListFilter, []string) {
	f := proposal.ListFilter{Status: c.Query("status"), Bucket: c.Query("bucket"), Cursor: c.Query("cursor"), Limit: pageSize}
	var errs []string
	if f.Status != "" && !proposal.ValidStatus(f.Status) {
		errs = append(errs, "status は状態のコードのどれかを指定してください。")
		f.Status = ""
	}
	if f.Bucket != "" && f.Bucket != proposal.BucketStock && f.Bucket != proposal.BucketBondETF {
		errs = append(errs, "bucket は株式か債券ETFを指定してください。")
		f.Bucket = ""
	}
	today := h.now().In(query.JST)
	from := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, query.JST).AddDate(0, 0, 1-defaultRangeDays)
	to := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, query.JST)
	var err error
	if raw := c.Query("from"); raw != "" {
		if from, err = time.ParseInLocation(dateLayout, raw, query.JST); err != nil {
			errs = append(errs, "from は YYYY-MM-DD 形式の日付で指定してください。")
		}
	}
	if raw := c.Query("to"); raw != "" {
		if to, err = time.ParseInLocation(dateLayout, raw, query.JST); err != nil {
			errs = append(errs, "to は YYYY-MM-DD 形式の日付で指定してください。")
		}
	}
	if len(errs) == 0 && to.Before(from) {
		errs = append(errs, "to は from 以降の日付で指定してください。")
	}
	f.Range = query.Range{From: from, To: to.AddDate(0, 0, 1)}
	return f, errs
}

// nextHref is the list's URL for the page after cursor, keeping the filter.
func nextHref(f pages.ProposalFilter, cursor string) string {
	v := url.Values{"from": {f.From}, "to": {f.To}, "cursor": {cursor}}
	if f.Status != "" {
		v.Set("status", f.Status)
	}
	if f.Bucket != "" {
		v.Set("bucket", f.Bucket)
	}
	return "/order-proposals?" + v.Encode()
}

func statusOptions() []pages.ProposalOption {
	var out []pages.ProposalOption
	for _, s := range orderproposal.Statuses() {
		out = append(out, pages.ProposalOption{Value: s, Label: pages.ProposalStatusLabel(s)})
	}
	return out
}

func bucketOptions() []pages.ProposalOption {
	return []pages.ProposalOption{
		{Value: proposal.BucketStock, Label: "株式"},
		{Value: proposal.BucketBondETF, Label: "債券ETF"},
	}
}
