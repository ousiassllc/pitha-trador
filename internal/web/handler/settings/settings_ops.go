package settings

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// OperationalSettings is the subset of *opsettings.Service the Settings
// screen's 運用設定 section needs (issue #708): the effective value of one
// key, saving a new value and resetting a key to its default. Defined here
// so handler tests need no database.
type OperationalSettings interface {
	Get(ctx context.Context, key string) (opsettings.Value, error)
	Save(ctx context.Context, key, raw string) error
	Reset(ctx context.Context, key string) error
	// Broker is the effective broker selection and 立花 settings (issue
	// #734: the Setup screen and Setup Guard follow it).
	Broker(ctx context.Context) (config.BrokerSettings, error)
}

// WithOperationalSettings enables the 運用設定 section of `GET /settings`
// and the `POST`/`DELETE /ops-settings/:key` routes (SaveOps/ResetOps)
// backed by ops. Without it the section is hidden and the routes answer 404.
func (h *SettingsHandler) WithOperationalSettings(ops OperationalSettings) *SettingsHandler {
	h.ops = ops
	return h
}

const (
	opsSavedNotice = "保存しました。"
	opsResetNotice = "保存した値を削除し、既定に戻しました。"
)

// opsGroupProps builds one group's props, reading every field's effective
// value. A per-key read error degrades that row to its empty/default state
// (and is logged) instead of failing the whole Settings screen, like h.row
// does for secrets (issue #70).
func (h *SettingsHandler) opsGroupProps(ctx context.Context, group opsGroup, noticeKey, notice string) molecules.SettingGroupProps {
	props := molecules.SettingGroupProps{ID: group.id, Name: group.name, Description: group.description, Note: group.note}
	for _, field := range group.fields {
		rowNotice := ""
		if field.key == noticeKey {
			rowNotice = notice
		}
		props.Fields = append(props.Fields, h.opsRow(ctx, field, rowNotice))
	}
	return props
}

func (h *SettingsHandler) opsRow(ctx context.Context, field opsField, notice string) molecules.SettingFieldRowProps {
	row := molecules.SettingFieldRowProps{
		Key: field.key, Label: field.label, Hint: field.hint, Placeholder: field.placeholder, Options: field.options,
		Notice: notice, CSRFField: middleware.CSRFFormField, CSRFToken: middleware.CSRFToken(ctx),
	}
	value, err := h.ops.Get(ctx, field.key)
	if err != nil {
		slog.Error("settings: read operational setting; showing it as unset", "key", field.key, "error", err)
		row.DefaultLabel = "不明"
		return row
	}
	row.Value, row.Overridden, row.Warning = value.Current, value.Overridden, value.Warning
	row.DefaultLabel = defaultLabel(value.Default)
	if config.IsPrivateKeyPathKey(field.key) {
		// Like a secret: only 設定済み is shown back, never the stored path.
		row.Value, row.DefaultLabel = "", "未設定"
	}
	return row
}

// defaultLabel names what applies while nothing is stored; an empty default
// (the backup directory) is "disabled".
func defaultLabel(def string) string {
	if def == "" {
		return "未設定（無効）"
	}
	return def
}

func (h *SettingsHandler) opsGroupsProps(ctx context.Context) []molecules.SettingGroupProps {
	if h.ops == nil {
		return nil
	}
	groups := make([]molecules.SettingGroupProps, len(opsGroups))
	for i, group := range opsGroups {
		groups[i] = h.opsGroupProps(ctx, group, "", "")
	}
	return groups
}

// SaveOps implements `POST /ops-settings/:key`: it validates and stores the
// form's `value` under the single key in the path and touches no other key.
// An unknown key is a 404/400, a blank or invalid value is a 400 carrying the
// reason, and neither changes the store. It responds with the refreshed
// SettingFieldRow fragment plus an out-of-band copy of the group's status.
func (h *SettingsHandler) SaveOps(c *gin.Context) {
	key, ok := h.opsKey(c)
	if !ok {
		return
	}
	err := h.ops.Save(c.Request.Context(), key, c.PostForm("value"))
	var invalid *opsettings.InvalidValueError
	switch {
	case errors.Is(err, opsettings.ErrEmptyValue):
		shared.RespondPageError(c, http.StatusBadRequest, "値を入力してください（保存した値を消す場合は「既定に戻す」を使ってください）。")
		return
	case errors.As(err, &invalid):
		shared.RespondPageError(c, http.StatusBadRequest, "値が正しくありません: "+invalid.Reason)
		return
	case err != nil:
		slog.Error("settings: save operational setting", "key", key, "error", err)
		shared.RespondPageError(c, http.StatusInternalServerError, "保存に失敗しました。")
		return
	}
	h.renderOpsRow(c, key, opsSavedNotice)
}

// ResetOps implements `DELETE /ops-settings/:key`: it removes the single
// key's stored value so the default applies again.
func (h *SettingsHandler) ResetOps(c *gin.Context) {
	key, ok := h.opsKey(c)
	if !ok {
		return
	}
	if err := h.ops.Reset(c.Request.Context(), key); err != nil {
		slog.Error("settings: reset operational setting", "key", key, "error", err)
		shared.RespondPageError(c, http.StatusInternalServerError, "既定に戻すのに失敗しました。")
		return
	}
	h.renderOpsRow(c, key, opsResetNotice)
}

// opsKey returns the path's key when the operational settings are enabled
// and the key belongs to a group; otherwise it has answered the request.
func (h *SettingsHandler) opsKey(c *gin.Context) (string, bool) {
	if h.ops == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return "", false
	}
	key := c.Param("key")
	if _, ok := opsGroupByKey(key); !ok {
		shared.RespondPageError(c, http.StatusBadRequest, "不明な設定キーです。")
		return "", false
	}
	return key, true
}

// renderOpsRow answers a successful save/reset like renderRow does for
// secrets: an HTMX request gets the refreshed row plus the group's status
// badge out-of-band; the row form's plain `method="post"` fallback is
// redirected back to /settings.
func (h *SettingsHandler) renderOpsRow(c *gin.Context, key, notice string) {
	if c.GetHeader("HX-Request") != "true" {
		c.Redirect(http.StatusSeeOther, settingsReturnPath(c))
		return
	}
	ctx := c.Request.Context()
	if refreshesSetup(c, key) {
		c.Header("HX-Refresh", "true")
	}
	group, _ := opsGroupByKey(key)
	props := h.opsGroupProps(ctx, group, key, notice)
	var row molecules.SettingFieldRowProps
	for _, field := range props.Fields {
		if field.Key == key {
			row = field
		}
	}
	comps := []templ.Component{molecules.SettingFieldRow(row), molecules.SettingGroupStatus(props, true)}
	if settingsReturnPath(c) == "/setup" {
		comps = append(comps, molecules.SetupStatus(h.setupComplete(ctx), true))
	}
	shared.RenderHTML(c, http.StatusOK, templ.Join(comps...))
}
