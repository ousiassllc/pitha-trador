// Package jevprompt serves the Settings screen's Jev prompt section (issue
// #872): `GET /settings/jev-prompt-panel`, `POST /settings/jev-prompt/shadow`
// (switch, candidate version, daily cost cap) and `POST
// /settings/jev-prompt/promote` (the explicit promotion). Like every POST they
// pass the session cookie and CSRF check; nothing here is reachable without the
// operator's session, and nothing calls Promote automatically.
package jevprompt

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevshadow"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jevprompt"
	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	view "github.com/ousiassllc/pitha-trador/internal/web/organisms/jevprompt"
)

// Service is the subset of *jevprompt.Service the handlers need.
type Service interface {
	State(ctx context.Context) (jevprompt.State, error)
	SetEnabled(ctx context.Context, on bool) error
	SetCandidate(ctx context.Context, version string) error
	SetCap(ctx context.Context, usd float64) error
	ResetCap(ctx context.Context) error
	Promote(ctx context.Context, version string) (jevshadow.Promotion, error)
}

// Handler implements the Jev prompt Settings routes.
type Handler struct{ svc Service }

// New returns a Handler over svc.
func New(svc Service) *Handler { return &Handler{svc: svc} }

// Register mounts the routes on engine.
func (h *Handler) Register(engine *gin.Engine) {
	engine.GET("/settings/jev-prompt-panel", h.Panel)
	engine.POST("/settings/jev-prompt/shadow", h.SaveShadow)
	engine.POST("/settings/jev-prompt/promote", h.Promote)
}

// Panel implements `GET /settings/jev-prompt-panel`.
func (h *Handler) Panel(c *gin.Context) { h.render(c, "") }

// SaveShadow implements `POST /settings/jev-prompt/shadow`: form `enabled` (a
// checked checkbox sends a value, an unchecked one nothing), `candidate` ("" for
// none) and `cap` (USD; blank restores the proposed default). Invalid input is
// a 422 and nothing is saved.
func (h *Handler) SaveShadow(c *gin.Context) {
	ctx := c.Request.Context()
	capText := strings.TrimSpace(c.PostForm("cap"))
	var capUSD float64
	if capText != "" {
		v, err := strconv.ParseFloat(capText, 64)
		if err != nil {
			shared.RespondActionError(c, http.StatusUnprocessableEntity, "1 日の費用上限は数値（USD）で入力してください。保存していません。")
			return
		}
		capUSD = v
	}
	// Validate everything before writing anything, so a rejected form saves nothing.
	if capText != "" {
		if err := jevshadowsettings.ValidateCap(capUSD); err != nil {
			shared.RespondActionError(c, http.StatusUnprocessableEntity, "1 日の費用上限は 0 より大きく "+strconv.FormatFloat(jevshadowsettings.MaxDailyCostCapUSD, 'g', -1, 64)+" USD 以下で入力してください。保存していません。")
			return
		}
	}
	if err := h.svc.SetCandidate(ctx, c.PostForm("candidate")); err != nil {
		h.fail(c, err, "候補の版を保存できませんでした。")
		return
	}
	var err error
	if capText == "" {
		err = h.svc.ResetCap(ctx)
	} else {
		err = h.svc.SetCap(ctx, capUSD)
	}
	if err != nil {
		h.fail(c, err, "費用の上限を保存できませんでした。")
		return
	}
	if err := h.svc.SetEnabled(ctx, c.PostForm("enabled") != ""); err != nil {
		h.fail(c, err, "シャドーの有効・無効を保存できませんでした。")
		return
	}
	h.render(c, "保存しました。")
}

// Promote implements `POST /settings/jev-prompt/promote`: form `version` becomes
// the live prompt version, and the promotion is recorded (who: the GUI, when,
// previous version, new version).
func (h *Handler) Promote(c *gin.Context) {
	p, err := h.svc.Promote(c.Request.Context(), c.PostForm("version"))
	if err != nil {
		h.fail(c, err, "昇格できませんでした。")
		return
	}
	h.render(c, "ライブの版を "+p.FromVersion+" から "+p.ToVersion+" に昇格しました。")
}

func (h *Handler) fail(c *gin.Context, err error, message string) {
	if errors.Is(err, jevprompt.ErrInvalidInput) {
		shared.RespondActionError(c, http.StatusUnprocessableEntity, strings.TrimPrefix(err.Error(), jevprompt.ErrInvalidInput.Error()+": ")+"。変更していません。")
		return
	}
	slog.ErrorContext(c.Request.Context(), "jevprompt: "+message, "error", err)
	shared.RespondActionError(c, http.StatusInternalServerError, message)
}

func (h *Handler) render(c *gin.Context, notice string) {
	c.Header("Cache-Control", "no-store")
	ctx := c.Request.Context()
	st, err := h.svc.State(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "jevprompt: read state", "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "Jev プロンプトの状態を読み込めませんでした。")
		return
	}
	shared.RenderHTML(c, http.StatusOK, view.Panel(props(st, notice)))
}

func props(st jevprompt.State, notice string) view.PanelProps {
	p := view.PanelProps{
		ShadowRuns:       st.ShadowRuns,
		Enabled:          st.Enabled,
		LiveVersion:      st.LiveVersion,
		CandidateVersion: st.CandidateVersion,
		Versions:         st.Versions,
		CapUSD:           usd(st.CapUSD),
		CapIsDefault:     st.CapIsDefault,
		TodayDate:        st.Today.Date,
		TodayCalls:       st.Today.Calls,
		TodayUSD:         usd(st.Today.CostUSD),
		TotalCalls:       st.Total.Calls,
		TotalUSD:         usd(st.Total.CostUSD),
		Notice:           notice,
	}
	for _, d := range st.Daily {
		p.Daily = append(p.Daily, view.DayRow{Date: d.Date, Calls: d.Calls, USD: usd(d.CostUSD)})
	}
	if h := st.HaltToday; h != nil {
		p.HaltNote = "1 日の費用上限（" + usd(h.CapUSD) + " USD）に達したため、今日のシャドーは止まっています（費用 " + usd(h.SpentUSD) + " USD。ライブの判断は続いています。翌日に再開します）。"
	}
	for _, pr := range st.Promotions {
		by := "GUI"
		if pr.Actor == jevshadow.ActorMCP {
			by = "MCP（" + pr.Client + "）"
		}
		p.Promotions = append(p.Promotions, view.PromotionRow{At: atoms.FormatJST(pr.At), By: by, From: pr.FromVersion, To: pr.ToVersion})
	}
	return p
}

// usd renders a USD amount without trailing zeros but with enough digits for a
// sub-cent request cost.
// usd formats a USD amount with at most 8 decimals and no trailing zeros, so
// summed float noise (0.0054600000000000004) never reaches the screen.
func usd(v float64) string {
	s := strconv.FormatFloat(v, 'f', 8, 64)
	return strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
}
