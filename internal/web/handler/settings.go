package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
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
	// Set stores plaintext under key. It rejects an empty plaintext:
	// removal is the explicit Delete (internal/repository.SecretsRepository
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

// settingsFields is the Settings screen's managed keys in display
// order (issue #57 スコープ item 6). Label matches the literal names
// operators previously set in `.env` so the migration away from it stays
// recognizable. Every key here must be in config.AllowedSecretKeys (the
// per-key routes' allow-list), which settings_test.go asserts.
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
	{config.KeySolAPIKey, "SOL_API_KEY（任意）"},
	{config.KeySolBaseURL, "SOL_BASE_URL（任意）"},
	{config.KeyOpusAPIKey, "OPUS_API_KEY（任意）"},
	{config.KeyOpusBaseURL, "OPUS_BASE_URL（任意）"},
}

// setupOptionalKeys are the optional fields the Setup screen (`GET
// /setup`, issue #80) offers next to the three required ones.
var setupOptionalKeys = []string{config.KeySlackWebhookURL}

// SettingsHandler implements `GET /settings`, `GET /setup`, `POST`/`DELETE
// /settings/:key` and `GET /system/secrets-status` (issue #57 スコープ
// items 6-7, per-key save/delete from issue #79, Setup screen from issue
// #80).
type SettingsHandler struct {
	store SecretsStore
}

// NewSettingsHandler returns a SettingsHandler backed by store.
func NewSettingsHandler(store SecretsStore) *SettingsHandler {
	return &SettingsHandler{store: store}
}

// Page implements `GET /settings`: one molecules.SecretFieldRow per
// field, each showing only whether a value is currently stored, never the
// value itself (issue #57's decision). A per-key SecretsStore.Get error
// (e.g. an unreadable/corrupted stored value) no longer 500s the whole
// screen (issue #70: "設定画面が開かず、エラーになる" - a single bad row
// must not permanently lock the operator out of the one screen that could
// fix it) - h.row degrades that key to "unset" instead, so the screen
// still renders and accepts a fresh value.
func (h *SettingsHandler) Page(c *gin.Context) {
	ctx := c.Request.Context()
	fields := make([]molecules.SecretFieldRowProps, len(settingsFields))
	for i, field := range settingsFields {
		fields[i] = h.row(ctx, field.key, field.label, "")
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = pages.SettingsPage(pages.SettingsProps{Fields: fields}).Render(ctx, c.Writer)
}

// SetupPage implements `GET /setup` (issue #80, FR-SETUP-2): the three
// required keys plus setupOptionalKeys as molecules.SecretFieldRow forms
// that post to the very same `POST`/`DELETE /settings/:key` routes
// Settings uses - Setup has no save/delete implementation of its own.
// Complete reports whether every required key is stored, so the page can
// offer the way on to the normal screens (Setup Guard, middleware.
// SetupGuard, stops redirecting as soon as they are). `/setup` itself
// stays reachable after setup for re-entering values.
func (h *SettingsHandler) SetupPage(c *gin.Context) {
	ctx := c.Request.Context()
	keys := append(config.RequiredSecretKeys(), setupOptionalKeys...)
	fields := make([]molecules.SecretFieldRowProps, len(keys))
	for i, key := range keys {
		fields[i] = h.row(ctx, key, settingsLabel(key), "")
	}
	complete := len(h.unsetKeys(ctx, config.RequiredSecretKeys())) == 0
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = pages.SetupPage(pages.SetupProps{Fields: fields, Complete: complete}).Render(ctx, c.Writer)
}

// Save implements `POST /settings/:key`: it stores the form's `value`
// under the single key in the path and touches no other key (issue #79).
// A key outside config.AllowedSecretKeys or an empty value is a 400 and
// leaves the store untouched - blanking a field never deletes it; that is
// Delete's job. It responds with the refreshed SecretFieldRow fragment.
func (h *SettingsHandler) Save(c *gin.Context) {
	key := c.Param("key")
	if !config.IsAllowedSecretKey(key) {
		respondActionError(c, http.StatusBadRequest, "不明な設定キーです。")
		return
	}
	value := c.PostForm("value")
	if value == "" {
		respondActionError(c, http.StatusBadRequest, "値を入力してください（保存済みの値を消す場合は削除を使ってください）。")
		return
	}

	ctx := c.Request.Context()
	if err := h.store.Set(ctx, key, value); err != nil {
		slog.Error("settings: save secret", "key", key, "error", err)
		respondActionError(c, http.StatusInternalServerError, "保存に失敗しました。")
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
		respondActionError(c, http.StatusBadRequest, "不明な設定キーです。")
		return
	}

	if err := h.store.Delete(c.Request.Context(), key); err != nil {
		slog.Error("settings: delete secret", "key", key, "error", err)
		respondActionError(c, http.StatusInternalServerError, "削除に失敗しました。")
		return
	}
	h.renderRow(c, key, "削除しました。反映にはアプリの再起動が必要です。")
}

// Status implements `GET /system/secrets-status`: the header's
// `#config-banner` fragment (organisms.Header's doc comment, mirroring
// `#header-status`'s own `hx-get`/`hx-trigger="load"` self-correcting
// pattern). It only guides toward unset optional keys (SLACK_WEBHOOK_URL
// etc.): the required keys are enforced by Setup Guard's redirect to
// `/setup` instead (issue #80), so they never appear here. It renders
// nothing once every optional key is configured. A per-key
// SecretsStore.Get error degrades that key to "unset" (issue #70) rather
// than 500ing the banner on every page.
func (h *SettingsHandler) Status(c *gin.Context) {
	missing := h.unsetKeys(c.Request.Context(), config.OptionalSecretKeys())
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = organisms.SecretsBanner(missing).Render(c.Request.Context(), c.Writer)
}

// renderRow answers a successful Save/Delete. An HTMX request gets the
// refreshed SecretFieldRow fragment; the row form's plain `method="post"`
// fallback (JS disabled) would otherwise render a bare fragment as the
// whole page, so it is redirected back to the screen it came from instead
// (the notice is only shown on the HTMX path).
func (h *SettingsHandler) renderRow(c *gin.Context, key, notice string) {
	if c.GetHeader("HX-Request") != "true" {
		c.Redirect(http.StatusSeeOther, settingsReturnPath(c))
		return
	}
	ctx := c.Request.Context()
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	_ = molecules.SecretFieldRow(h.row(ctx, key, settingsLabel(key), notice)).Render(ctx, c.Writer)
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
	return molecules.SecretFieldRowProps{Key: key, Label: label, Configured: ok, Notice: notice}
}

// settingsLabel returns key's display label; key must be in
// config.AllowedSecretKeys.
func settingsLabel(key string) string {
	for _, field := range settingsFields {
		if field.key == key {
			return field.label
		}
	}
	return key
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

// settingsReturnPath is where a non-HTMX Save/Delete goes back to: `/setup`
// when the form was submitted from the Setup screen, `/settings` otherwise.
// Only these two fixed paths are ever returned, never the raw Referer.
func settingsReturnPath(c *gin.Context) string {
	if ref, err := url.Parse(c.GetHeader("Referer")); err == nil && ref.Path == "/setup" {
		return "/setup"
	}
	return "/settings"
}
