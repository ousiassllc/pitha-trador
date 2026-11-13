// Package jevtrial serves the Settings screen's model-comparison trial
// section: `GET /settings/jev-trial-panel` and `POST /settings/jev-trial`
// (switch, arms, overall daily cost cap). Like every POST it passes the session
// cookie and CSRF check. This is the only route that can turn the trial on:
// nothing in the MCP writes these settings.
package jevtrial

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	view "github.com/ousiassllc/pitha-trador/internal/web/organisms/jevtrial"
)

// Service is the subset of *jevtrial.Service the handlers need.
type Service interface {
	State(ctx context.Context) (jevtrial.State, error)
	Save(ctx context.Context, f jevtrial.Form) error
}

// Handler implements the trial Settings routes.
type Handler struct{ svc Service }

// New returns a Handler over svc.
func New(svc Service) *Handler { return &Handler{svc: svc} }

// Register mounts the routes on engine.
func (h *Handler) Register(engine *gin.Engine) {
	engine.GET("/settings/jev-trial-panel", h.Panel)
	engine.POST("/settings/jev-trial", h.Save)
}

// formError is the inline-alert slot of the form (view.Panel): a rejected form
// puts its reason there instead of in a toast.
const formError = "#jev-trial-error"

// Panel implements `GET /settings/jev-trial-panel`.
func (h *Handler) Panel(c *gin.Context) { h.render(c, "") }

// Save implements `POST /settings/jev-trial`: form `enabled` (a checked checkbox
// sends a value, an unchecked one nothing), `arms` (a JSON array; blank for no
// arm) and `cap` (USD; blank restores the default). Invalid input is a 422 and
// nothing is saved.
func (h *Handler) Save(c *gin.Context) {
	form := jevtrial.Form{Enabled: c.PostForm("enabled") != "", ArmsJSON: c.PostForm("arms")}
	if capText := strings.TrimSpace(c.PostForm("cap")); capText != "" {
		v, err := strconv.ParseFloat(capText, 64)
		if err != nil {
			shared.RespondFormError(c, formError, "1 日の費用上限は数値（USD）で入力してください。保存していません。")
			return
		}
		form.Cap = &v
	}
	if err := h.svc.Save(c.Request.Context(), form); err != nil {
		if errors.Is(err, jevtrial.ErrInvalidInput) {
			shared.RespondFormError(c, formError, strings.TrimPrefix(err.Error(), jevtrial.ErrInvalidInput.Error()+": ")+"。保存していません。")
			return
		}
		slog.ErrorContext(c.Request.Context(), "jevtrial: save", "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "トライアルの設定を保存できませんでした。")
		return
	}
	h.render(c, "保存しました。")
}

func (h *Handler) render(c *gin.Context, notice string) {
	c.Header("Cache-Control", "no-store")
	ctx := c.Request.Context()
	st, err := h.svc.State(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "jevtrial: read state", "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "トライアルの状態を読み込めませんでした。")
		return
	}
	shared.RenderHTML(c, http.StatusOK, view.Panel(props(st, notice)))
}

func props(st jevtrial.State, notice string) view.PanelProps {
	p := view.PanelProps{
		TrialRuns: st.TrialRuns, Enabled: st.Enabled,
		CapUSD: usd(st.CapUSD), CapIsDefault: st.CapIsDefault,
		ArmsJSON: st.ArmsJSON, ArmsError: st.ArmsError,
		KeyKnown: st.KeyKnown, KeySet: st.KeySet,
		TodayDate: st.TodayDate, TodayCalls: st.Today.Calls, TodayUSD: usd(st.Today.CountedCostUSD),
		TotalCalls: st.Total.Calls, TotalUSD: usd(st.Total.CountedCostUSD),
		HaltNote: wholeHaltNote(st.Halt),
		Notice:   notice,
	}
	for _, a := range st.Arms {
		p.Arms = append(p.Arms, view.ArmRow{
			ID: a.Arm.ID, Model: a.Arm.Model, PromptVersion: a.Arm.PromptVersion,
			TodayCalls: a.Today.Calls, TodayUSD: usd(a.Today.CountedCostUSD), CapUSD: usd(a.CapUSD),
			FailuresToday: a.FailuresToday, FailureCap: a.FailureCap,
			Status: armStatus(a),
		})
	}
	return p
}

// wholeHaltNote says why the whole trial is stopped today ("" for none).
func wholeHaltNote(h *jevtrialsettings.Halt) string {
	if h == nil {
		return ""
	}
	switch h.Reason {
	case jevtrialsettings.HaltReasonPaymentRequired:
		return "OpenRouter が 402（支払い不足）を返したため、今日のトライアル全体は止まっています（ライブの判断は続いています。翌日に再開します）。"
	case jevtrialsettings.HaltReasonDailyCostCap:
		return "全体の 1 日の費用上限（" + usd(h.CapUSD) + " USD）に達したため、今日のトライアル全体は止まっています（数えた費用 " + usd(h.SpentUSD) + " USD。ライブの判断は続いています。翌日に再開します）。"
	}
	return "今日のトライアル全体は止まっています（ライブの判断は続いています。翌日に再開します）。"
}

// armStatus says why an arm is stopped today ("" while it runs): its own stop
// record first, then an active 429/529 cooldown.
func armStatus(a jevtrial.ArmStatus) string {
	if h := a.Halt; h != nil {
		switch h.Reason {
		case jevtrialsettings.HaltReasonArmCostCap:
			return "このアームの上限（" + usd(h.CapUSD) + " USD）に達したため停止"
		case jevtrialsettings.HaltReasonDailyFailureCap:
			return "失敗が 1 日の上限（" + strconv.Itoa(h.FailureCap) + " 回）に達したため停止"
		case jevtrialsettings.HaltReasonPaymentRequired:
			return "402（支払い不足）のため停止"
		case jevtrialsettings.HaltReasonForbidden:
			return "403（権限なし）のため停止"
		case jevtrialsettings.HaltReasonDailyCostCap:
			return "全体の費用上限に達したため停止"
		}
		return "停止"
	}
	if a.ThrottleActive && a.Throttle != nil {
		until, _ := time.Parse(time.RFC3339, a.Throttle.Until)
		return strconv.Itoa(a.Throttle.Status) + "（レート制限・過負荷）のため " + atoms.FormatJST(until) + " まで停止"
	}
	return ""
}

// usd formats a USD amount with at most 8 decimals and no trailing zeros, so
// summed float noise never reaches the screen.
func usd(v float64) string {
	s := strconv.FormatFloat(v, 'f', 8, 64)
	return strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
}
