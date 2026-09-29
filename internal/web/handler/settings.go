package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// SecretsStore is the subset of internal/repository.SecretsRepository's
// methods the Settings screen and the header's secrets-status banner
// need. An interface here - rather than importing internal/repository's
// concrete type - keeps this package's dependency direction unchanged
// (docs/architecture/overview.md §3: "web/handler: service, domainに依存。
// repositoryを直接使わない"); *repository.SecretsRepository implements it
// without either package needing to name the other, the same pattern
// CalibrationSource/SystemEngine/BacktestRunner already use for their own
// concrete internal/service types.
type SecretsStore interface {
	// Get returns key's decrypted plaintext value, or ("", false, nil)
	// when unset. Values are never shown back to the operator (Page only
	// renders whether ok is true) - see SettingsHandler.Page.
	Get(ctx context.Context, key string) (plaintext string, ok bool, err error)
	// Set stores plaintext under key. An empty plaintext clears it
	// (internal/repository.SecretsRepository.Set's doc comment).
	Set(ctx context.Context, key, plaintext string) error
}

// StaticSecretsStore is internal/router.New()'s default SecretsStore
// (router-level tests only): every key reports unset, and Set is a no-op.
type StaticSecretsStore struct{}

func (StaticSecretsStore) Get(context.Context, string) (string, bool, error) { return "", false, nil }
func (StaticSecretsStore) Set(context.Context, string, string) error         { return nil }

// settingsFields is the Settings screen's managed keys in display
// order (issue #57 スコープ item 6). Label doubles as the `<input name>`
// (config.Key* constants), matching the literal names operators
// previously set in `.env` so the migration away from it stays
// recognizable.
var settingsFields = []struct {
	key   string
	label string
}{
	{config.KeyJevAPIKey, "JEV_API_KEY"},
	{config.KeyJevBaseURL, "JEV_BASE_URL"},
	{config.KeyKabuAPIPassword, "KABU_API_PASSWORD"},
	{config.KeySlackWebhookURL, "SLACK_WEBHOOK_URL（任意）"},
	{config.KeyLunaAPIKey, "LUNA_API_KEY（任意）"},
	{config.KeyLunaBaseURL, "LUNA_BASE_URL（任意）"},
	{config.KeyNewsFeedURL, "NEWS_FEED_URL（任意）"},
	{config.KeyNewsFeedAPIKey, "NEWS_FEED_API_KEY（任意）"},
}

// requiredSettingsKeys mirrors config.LoadSecretsFromDB's own required
// set (SLACK_WEBHOOK_URL excluded) - both the secrets-status banner and
// Settings' "missing" reporting must agree with what actually makes
// BuildServices' Jev/kabuステーションAPI clients functional.
var requiredSettingsKeys = []string{config.KeyJevAPIKey, config.KeyJevBaseURL, config.KeyKabuAPIPassword}

// SettingsHandler implements `GET /settings`, `POST /settings` and `GET
// /system/secrets-status` (issue #57 スコープ items 6-7).
type SettingsHandler struct {
	store SecretsStore
}

// NewSettingsHandler returns a SettingsHandler backed by store.
func NewSettingsHandler(store SecretsStore) *SettingsHandler {
	return &SettingsHandler{store: store}
}

// Page implements `GET /settings`: each field shows only whether a value
// is currently stored, never the value itself (issue #57's decision). A
// per-key SecretsStore.Get error (e.g. an unreadable/corrupted stored
// value) no longer 500s the whole screen (issue #70: "設定画面が開かず、
// エラーになる" - a single bad row must not permanently lock the operator
// out of the one screen that could fix it) - h.props degrades that key to
// "unset" instead, so the screen still renders and accepts a fresh value.
func (h *SettingsHandler) Page(c *gin.Context) {
	props := h.props(c.Request.Context(), false)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = pages.SettingsPage(props).Render(c.Request.Context(), c.Writer)
}

// Save implements `POST /settings`: every submitted field is stored
// verbatim via SecretsStore.Set, including blank ones - submitting a
// field empty clears its stored value rather than leaving the existing
// one untouched (internal/repository.SecretsRepository.Set's doc
// comment; internal/web/pages.SettingsPage's form explains this to the
// operator). It then re-renders the form with a "saved" notice.
func (h *SettingsHandler) Save(c *gin.Context) {
	ctx := c.Request.Context()
	for _, field := range settingsFields {
		if err := h.store.Set(ctx, field.key, c.PostForm(field.key)); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
	}

	props := h.props(ctx, true)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = pages.SettingsPage(props).Render(ctx, c.Writer)
}

// Status implements `GET /system/secrets-status`: the header's
// `#config-banner` fragment (organisms.Header's doc comment, mirroring
// `#header-status`'s own `hx-get`/`hx-trigger="load"` self-correcting
// pattern). It renders nothing once every required key is configured. A
// per-key SecretsStore.Get error degrades that key to "missing" (issue
// #70) rather than 500ing the banner on every page.
func (h *SettingsHandler) Status(c *gin.Context) {
	missing := h.missingRequiredKeys(c.Request.Context())
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = organisms.SecretsBanner(missing).Render(c.Request.Context(), c.Writer)
}

func (h *SettingsHandler) props(ctx context.Context, saved bool) pages.SettingsProps {
	fields := make([]pages.SettingsField, len(settingsFields))
	for i, field := range settingsFields {
		_, ok, err := h.store.Get(ctx, field.key)
		if err != nil {
			// issue #70: an unreadable stored value (e.g. a corrupted/
			// undecryptable row, or a transient "database is locked")
			// must not brick the whole Settings screen - treat it as
			// unset instead so the operator can still open Settings and
			// re-save it (self-healing: Save's plain Set overwrites
			// whatever was there).
			slog.Error("settings: read stored secret; treating as unset so the screen still renders", "key", field.key, "error", err)
			ok = false
		}
		fields[i] = pages.SettingsField{Key: field.key, Label: field.label, Configured: ok}
	}
	return pages.SettingsProps{Fields: fields, Saved: saved}
}

func (h *SettingsHandler) missingRequiredKeys(ctx context.Context) []string {
	var missing []string
	for _, key := range requiredSettingsKeys {
		_, ok, err := h.store.Get(ctx, key)
		if err != nil {
			slog.Error("settings: read stored secret for secrets-status banner; treating as unset", "key", key, "error", err)
			ok = false
		}
		if !ok {
			missing = append(missing, key)
		}
	}
	return missing
}
