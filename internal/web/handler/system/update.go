package system

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/version"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// UpdateController is the subset of internal/service/updater's
// SchedulerAdapter the update notification routes need (issue #76):
// Status is the last check's outcome, CheckForUpdate runs a check right
// now - the same call the scheduler's periodic tick makes, so a manual
// check that finds a verified installer restarts the app exactly like the
// automatic one. updater.SchedulerAdapter implements it directly.
type UpdateController interface {
	Status() updater.Status
	CheckForUpdate(ctx context.Context) error
}

// UpdateHandler implements `GET /system/update-status`, `GET
// /system/update-panel` and `POST /system/update-check` (issue #76). A nil
// controller means this build has no updater (cmd/server): the status GET
// renders nothing, the panel GET renders a notice saying so (issue #241),
// and the POST route 404s.
type UpdateHandler struct {
	controller UpdateController
}

// NewUpdateHandler returns an UpdateHandler backed by controller (nil for
// builds without an updater).
func NewUpdateHandler(controller UpdateController) *UpdateHandler {
	return &UpdateHandler{controller: controller}
}

// updateStatusChangedEvent is the HX-Trigger event Check fires so the
// Header's `#update-banner` refreshes immediately instead of at its next
// poll (organisms.Header's doc comment).
const updateStatusChangedEvent = "updateStatusChanged"

// Status implements `GET /system/update-status`: Header's `#update-banner`
// fragment (organisms.UpdateBanner).
func (h *UpdateHandler) Status(c *gin.Context) {
	var props organisms.UpdateBannerProps
	if h.controller != nil {
		status := h.controller.Status()
		props = organisms.UpdateBannerProps{
			Available: status.Available,
			Version:   status.Version,
			Blocked:   status.Blocked,
			Ready:     status.Ready,
		}
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = organisms.UpdateBanner(props).Render(c.Request.Context(), c.Writer)
}

// Panel implements `GET /system/update-panel`: Settings' `#update-panel`
// fragment (organisms.UpdatePanel); without an updater it only says the
// build has none.
func (h *UpdateHandler) Panel(c *gin.Context) {
	h.renderPanel(c, false)
}

// Check implements `POST /system/update-check`: runs an update check
// immediately, then renders the refreshed UpdatePanel. A failed check is
// reported in the panel (the Checker already logged and recorded it in its
// Status) rather than as an HTTP error, so HTMX still swaps the fragment.
func (h *UpdateHandler) Check(c *gin.Context) {
	if h.controller == nil {
		shared.RespondActionError(c, http.StatusNotFound, "アップデート機能は利用できません。")
		return
	}
	err := h.controller.CheckForUpdate(c.Request.Context())
	if err != nil {
		slog.Error("update: manual check failed", "error", err)
	}
	c.Header("HX-Trigger", updateStatusChangedEvent)
	h.renderPanel(c, err != nil)
}

func (h *UpdateHandler) renderPanel(c *gin.Context, failed bool) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	if h.controller == nil {
		_ = organisms.UpdatePanel(organisms.UpdatePanelProps{
			CurrentVersion: version.Version,
			Unavailable:    true,
		}).Render(c.Request.Context(), c.Writer)
		return
	}
	status := h.controller.Status()
	props := organisms.UpdatePanelProps{
		CurrentVersion: version.Version,
		DevBuild:       status.DevBuild,
		Failed:         failed || status.LastError != "",
		Available:      status.Available,
		Version:        status.Version,
		Blocked:        status.Blocked,
		Ready:          status.Ready,
	}
	if props.Failed {
		props.ErrorReason = errorReason(status.ErrorKind)
	}
	if props.Blocked {
		props.BlockedReason = blockedReason(status.BlockedKind)
	}
	if !status.CheckedAt.IsZero() {
		props.CheckedAt = status.CheckedAt.In(time.Local).Format("2006-01-02 15:04")
	}
	_ = organisms.UpdatePanel(props).Render(c.Request.Context(), c.Writer)
}

// blockedReason words the safety-gate condition holding a newer release
// back (issue #241); the gate's own log detail stays in the log.
func blockedReason(kind updater.BlockKind) string {
	switch kind {
	case updater.BlockOpenPositions:
		return "ポジションを保有しているため"
	case updater.BlockKillSwitch:
		return "Kill Switch が発動しているため"
	case updater.BlockRecentOrder:
		return "直近に発注があったため"
	case updater.BlockCheckFailed:
		return "安全条件を確認できなかったため"
	default:
		return "理由を特定できません"
	}
}

// errorReason words why a check failed (issue #241) without exposing the
// raw error text, which may carry URLs or local paths; that stays in the
// log.
func errorReason(kind updater.ErrorKind) string {
	switch kind {
	case updater.ErrorNetwork:
		return "ネットワークまたは GitHub 側の問題で接続できませんでした"
	case updater.ErrorRateLimit:
		return "GitHub API のレート制限に達しました"
	case updater.ErrorAccess:
		return "GitHub のリリース情報にアクセスできません（リポジトリが非公開の場合は、設定画面の UPDATE_GITHUB_TOKEN にアクセストークンを保存して再起動してください）"
	case updater.ErrorAuth:
		return "GitHub が設定済みの UPDATE_GITHUB_TOKEN を受け付けませんでした（有効期限・権限・対象リポジトリへのアクセス権を確認してください）"
	case updater.ErrorVerification:
		return "ダウンロードしたインストーラーの検証に失敗しました（更新は中止されました）"
	case updater.ErrorRelease:
		return "取得したリリース情報の内容が不正です（タグ名またはインストーラー資産を確認してください）"
	default:
		return "原因を特定できませんでした（詳細はログを参照してください）"
	}
}
