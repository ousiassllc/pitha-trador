// Package jevtrial serves the Settings screen's model-comparison trial
// section: `GET /settings/jev-trial-panel` and `POST /settings/jev-trial`
// (switch, arms, overall daily cost cap) and the arm form's add / remove row
// fragments. Like every POST it passes the session
// cookie and CSRF check. The switch, arms, caps and Codex settings it saves are
// the six operator keys the MCP's settings_set also writes (issue #1220); the
// service it is wired to records a save in the MCP's audit log as actor gui.
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
	engine.POST("/settings/jev-trial/arms/add", h.AddArm)
	engine.POST("/settings/jev-trial/arms/remove", h.RemoveArm)
}

// formError is the inline-alert slot of the form (view.Panel): a rejected form
// puts its reason there instead of in a toast.
const formError = "#jev-trial-error"

// Panel implements `GET /settings/jev-trial-panel`.
func (h *Handler) Panel(c *gin.Context) { h.render(c, "") }

// Save implements `POST /settings/jev-trial`: form `enabled` (a checked checkbox
// sends a value, an unchecked one nothing), the arm rows (`arm-<n>-<field>`, see
// readRows; none for no arm) and `cap` (USD; blank restores the default). The
// rows become arms that go through the service's one validation. Invalid input
// is a 422 and nothing is saved.
func (h *Handler) Save(c *gin.Context) {
	rows, err := readRows(c)
	if err != nil {
		shared.RespondFormError(c, formError, err.Error()+"。保存していません。")
		return
	}
	arms, err := buildArms(rows)
	if err != nil {
		shared.RespondFormError(c, formError, err.Error()+"。保存していません。")
		return
	}
	form := jevtrial.Form{Enabled: c.PostForm("enabled") != "", Arms: arms, CodexEnabled: c.PostForm("codex_enabled") != ""}
	var ok bool
	if form.CodexCallCap, ok = codexCap(c, "codex_call_cap"); !ok {
		return
	}
	if form.CodexTokenCap, ok = codexCap(c, "codex_token_cap"); !ok {
		return
	}
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

// codexCap reads a Codex cap field: nil for a blank one (the default applies). A
// value that is not an integer is a 422 (ok false) and nothing is saved.
func codexCap(c *gin.Context, field string) (v *int, ok bool) {
	text := strings.TrimSpace(c.PostForm(field))
	if text == "" {
		return nil, true
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		shared.RespondFormError(c, formError, "ChatGPT サブスクの 1 日の上限は整数で入力してください。保存していません。")
		return nil, false
	}
	return &n, true
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
		Editor: editorProps(storedRows(st)), ArmsError: st.ArmsError,
		KeyKnown: st.KeyKnown, KeySet: st.KeySet,
		TodayDate: st.TodayDate, TodayCalls: st.Today.Calls, TodayUSD: usd(st.Today.CountedCostUSD),
		TotalCalls: st.Total.Calls, TotalUSD: usd(st.Total.CountedCostUSD),
		HaltNote: wholeHaltNote(st.Halt),
		Codex:    codexProps(st.Codex),
		Notice:   notice,
	}
	for _, a := range st.Arms {
		status := armStatus(a)
		if a.Arm.Provider == jevtrialsettings.ProviderCodexChatGPT && status == "" {
			status = codexArmNote(st.Codex)
		}
		p.Arms = append(p.Arms, view.ArmRow{
			ID: a.Arm.ID, Model: a.Arm.Model, PromptVersion: a.Arm.PromptVersion,
			TodayCalls: a.Today.Calls, TodayUSD: usd(a.Today.CountedCostUSD), CapUSD: usd(a.CapUSD),
			FailuresToday: a.FailuresToday, FailureCap: a.FailureCap,
			Status: status, Subscription: a.Arm.Provider == jevtrialsettings.ProviderCodexChatGPT,
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
