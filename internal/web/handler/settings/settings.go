package settings

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// SecretsStore is the subset of internal/repository/system.SecretsRepository's
// methods the Settings screen and the Home screen's 要対応 list need. An interface here - rather than importing internal/repository's
// concrete type - keeps this package's dependency direction unchanged
// (docs/architecture/overview.md §3: "web/handler: service, domainに依存。
// repositoryを直接使わない"); *system.SecretsRepository implements it
// without either package needing to name the other, the same pattern
// CalibrationSource/SystemEngine/BacktestRunner already use for their own
// concrete internal/service types.
type SecretsStore interface {
	// Get returns key's decrypted plaintext value, or ("", false, nil)
	// when unset. Values are never shown back to the operator (Page only
	// renders whether ok is true) - see SettingsHandler.Page.
	Get(ctx context.Context, key string) (plaintext string, ok bool, err error)
	// Set stores plaintext under key. It rejects an empty plaintext:
	// removal is the explicit Delete (internal/repository/system.SecretsRepository
	// .Set's doc comment).
	Set(ctx context.Context, key, plaintext string) error
	// Delete removes key's stored value; deleting an unset key is not an
	// error.
	Delete(ctx context.Context, key string) error
}

// StaticSecretsStore is internal/router.New()'s default SecretsStore
// (router-level tests only): every key reports unset, and Set/Delete are
// no-ops.
type StaticSecretsStore struct{}

func (StaticSecretsStore) Get(context.Context, string) (string, bool, error) { return "", false, nil }
func (StaticSecretsStore) Set(context.Context, string, string) error         { return nil }
func (StaticSecretsStore) Delete(context.Context, string) error              { return nil }

// SettingsHandler implements `GET /settings`, `GET /setup`, `POST`/`DELETE
// /settings/:key` (issue #57 スコープ items 6-7, per-key save/delete from
// issue #79, Setup screen from issue #80).
type SettingsHandler struct {
	store SecretsStore
	ops   OperationalSettings // nil until WithOperationalSettings
	// session is the running broker adapter, for the 立花 card's session
	// state (nil until WithBrokerSession).
	session BrokerSession
	// mcp shows the 外部連携 section's MCP card (issue #792); the panel
	// itself is served by internal/web/handler/mcpsettings.
	mcp bool
	// jevPrompt shows the Jev prompt card (issue #872); the panel itself is
	// served by internal/web/handler/jevprompt.
	jevPrompt bool
}

// NewSettingsHandler returns a SettingsHandler backed by store.
func NewSettingsHandler(store SecretsStore) *SettingsHandler {
	return &SettingsHandler{store: store}
}

// WithMCPSettings shows the MCP card (modal `modal-mcp`) on the Settings
// screen; call it only when the MCP panel routes are registered.
func (h *SettingsHandler) WithMCPSettings() *SettingsHandler {
	h.mcp = true
	return h
}

// WithJevPromptSettings shows the Jev prompt card (modal `modal-jev-prompt`)
// on the Settings screen; call it only when the panel routes are registered.
func (h *SettingsHandler) WithJevPromptSettings() *SettingsHandler {
	h.jevPrompt = true
	return h
}

// Save implements `POST /settings/:key`: it stores the form's `value`
// under the single key in the path and touches no other key (issue #79).
// The value is trimmed and validated per key (config.NormalizeSecretValue,
// issue #235: URL keys need an http/https URL with a host, credentials no
// control characters). A key outside config.AllowedSecretKeys, an empty
// value or an invalid value is a 400 and leaves the store untouched -
// blanking a field never deletes it; that is Delete's job. It responds
// with the refreshed SecretFieldRow fragment.
func (h *SettingsHandler) Save(c *gin.Context) {
	key := c.Param("key")
	if !config.IsAllowedSecretKey(key) {
		shared.RespondPageError(c, http.StatusBadRequest, "不明な設定キーです。")
		return
	}
	value, err := config.NormalizeSecretValue(key, c.PostForm("value"))
	if errors.Is(err, config.ErrEmptySecretValue) {
		shared.RespondPageError(c, http.StatusBadRequest, "値を入力してください（保存済みの値を消す場合は削除を使ってください）。")
		return
	}
	if err != nil {
		shared.RespondPageError(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()
	if err := h.store.Set(ctx, key, value); err != nil {
		slog.Error("settings: save secret", "key", key, "error", err)
		shared.RespondPageError(c, http.StatusInternalServerError, "保存に失敗しました。")
		return
	}
	h.renderRow(c, key, "保存しました。反映にはアプリの再起動が必要です。")
}

// Delete implements `DELETE /settings/:key`: it removes the single key in
// the path and touches no other key (issue #79). A key outside
// config.AllowedSecretKeys is a 400. It responds with the refreshed
// SecretFieldRow fragment.
func (h *SettingsHandler) Delete(c *gin.Context) {
	key := c.Param("key")
	if !config.IsAllowedSecretKey(key) {
		shared.RespondPageError(c, http.StatusBadRequest, "不明な設定キーです。")
		return
	}

	if err := h.store.Delete(c.Request.Context(), key); err != nil {
		slog.Error("settings: delete secret", "key", key, "error", err)
		shared.RespondPageError(c, http.StatusInternalServerError, "削除に失敗しました。")
		return
	}
	h.renderRow(c, key, "削除しました。反映にはアプリの再起動が必要です。")
}

// UnsetOptionalKeys lists the optional secrets-table keys that have no stored
// value yet (not defaultedKeys: their unset state is normal). The Home
// screen's 要対応 list reads it (issue #896). Required keys never appear here:
// Setup Guard redirects to `/setup` while one is unset (issue #80). A per-key
// SecretsStore.Get error degrades that key to "unset" (issue #70).
func (h *SettingsHandler) UnsetOptionalKeys(ctx context.Context) []string {
	var keys []string
	for _, key := range config.OptionalSecretKeys() {
		if !slices.Contains(defaultedKeys, key) {
			keys = append(keys, key)
		}
	}
	return h.unsetKeys(ctx, keys)
}

// secretsStatusChangedEvent is the HX-Trigger event a successful Save/Delete
// fires so the Header's `#attention-badge` and Home's 要対応 list refresh
// immediately instead of staying stale until their next poll (organisms.Header's
// doc comment).
const secretsStatusChangedEvent = "secretsStatusChanged"

// renderRow answers a successful Save/Delete. An HTMX request gets the
// refreshed SecretFieldRow fragment plus an out-of-band copy of the owning
// connection's status badge, so the list behind the open modal shows the
// new 設定済み/未設定 state (issue #302). When the request came from the
// Setup screen it also carries the recomputed completion message, so it
// follows the required badges (issue #325). The row form's plain
// `method="post"` fallback (JS disabled) would otherwise render a bare
// fragment as the whole page, so it is redirected back to the screen it
// came from instead (the notice is only shown on the HTMX path). The
// response also fires secretsStatusChangedEvent so the 要対応 badge and list
// refetch the unset-optional-keys list (issue #695).
func (h *SettingsHandler) renderRow(c *gin.Context, key, notice string) {
	returnPath := settingsReturnPath(c)
	if c.GetHeader("HX-Request") != "true" {
		c.Redirect(http.StatusSeeOther, returnPath)
		return
	}
	ctx := c.Request.Context()
	c.Header("HX-Trigger", secretsStatusChangedEvent)
	comps := []templ.Component{molecules.SecretFieldRow(h.row(ctx, key, settingsLabel(key), notice))}
	if conn, ok := connectionByKey(key); ok {
		comps = append(comps, molecules.ConnectionStatus(h.connectionProps(ctx, conn, h.requiredSecretKeys(ctx)), true))
	}
	if returnPath == "/setup" {
		comps = append(comps, molecules.SetupStatus(h.setupComplete(ctx), true))
	}
	shared.RenderHTML(c, http.StatusOK, templ.Join(comps...))
}

// connections builds the props of every connection in conns, in order.
func (h *SettingsHandler) connections(ctx context.Context, conns []connection) []molecules.ConnectionProps {
	required, _ := h.requirements(ctx)
	props := make([]molecules.ConnectionProps, len(conns))
	for i, conn := range conns {
		props[i] = h.connectionProps(ctx, conn, required.SecretKeys)
	}
	return props
}

// connectionProps builds one connection's props; it is Required when it
// holds one of the required secrets keys (which depend on the selected
// broker, issue #734).
func (h *SettingsHandler) connectionProps(ctx context.Context, conn connection, required []string) molecules.ConnectionProps {
	props := molecules.ConnectionProps{ID: conn.id, Name: conn.name, Description: conn.description, DefaultLabel: conn.defaultLabel}
	if conn.id == tachibanaConnectionID {
		props.Badges, props.Details = h.tachibanaStatus(ctx)
	}
	for _, field := range conn.fields {
		props.Fields = append(props.Fields, h.row(ctx, field.key, field.label, ""))
		if slices.Contains(required, field.key) {
			props.Required = true
		}
	}
	return props
}

func (h *SettingsHandler) row(ctx context.Context, key, label, notice string) molecules.SecretFieldRowProps {
	_, ok, err := h.store.Get(ctx, key)
	if err != nil {
		// issue #70: an unreadable stored value (e.g. a corrupted/
		// undecryptable row, or a transient "database is locked")
		// must not brick the whole Settings screen - treat it as
		// unset instead so the operator can still open Settings and
		// re-save it (self-healing: Save's Set overwrites whatever
		// was there).
		slog.Error("settings: read stored secret; treating as unset so the screen still renders", "key", key, "error", err)
		ok = false
	}
	return molecules.SecretFieldRowProps{Key: key, Label: label, Hint: settingsHints[key], Configured: ok, Notice: notice, CSRFField: middleware.CSRFFormField, CSRFToken: middleware.CSRFToken(ctx)}
}

// settingsLabel returns key's display label; key must be in
// config.AllowedSecretKeys.
func settingsLabel(key string) string {
	if conn, ok := connectionByKey(key); ok {
		for _, field := range conn.fields {
			if field.key == key {
				return field.label
			}
		}
	}
	return key
}

// connectionByKey returns the connection whose modal holds key.
func connectionByKey(key string) (connection, bool) {
	for _, conn := range settingsConnections {
		if slices.ContainsFunc(conn.fields, func(f settingsField) bool { return f.key == key }) {
			return conn, true
		}
	}
	return connection{}, false
}

// connectionByID returns the connection with the given slug.
func connectionByID(id string) (connection, bool) {
	for _, conn := range settingsConnections {
		if conn.id == id {
			return conn, true
		}
	}
	return connection{}, false
}

// unsetKeys returns the subset of keys with no stored value, in order. A
// Get error counts as unset (issue #70).
func (h *SettingsHandler) unsetKeys(ctx context.Context, keys []string) []string {
	var missing []string
	for _, key := range keys {
		_, ok, err := h.store.Get(ctx, key)
		if err != nil {
			slog.Error("settings: read stored secret; treating as unset", "key", key, "error", err)
			ok = false
		}
		if !ok {
			missing = append(missing, key)
		}
	}
	return missing
}
