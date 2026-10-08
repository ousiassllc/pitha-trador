package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// SetupPath is the first-run Setup screen (`GET /setup`) the Setup Guard
// redirects to.
const SetupPath = "/setup"

// SecretsReader is the subset of internal/web/handler/settings.SecretsStore the
// Setup Guard needs. An interface here keeps internal/web/middleware from
// depending on internal/repository directly (docs/architecture/overview.md
// §3 layer rule).
type SecretsReader interface {
	Get(ctx context.Context, key string) (plaintext string, ok bool, err error)
}

// SetupRequirementsFunc returns what must be stored right now. It is called
// per request, so a broker selection saved on `/setup` takes effect at once
// (issue #734).
type SetupRequirementsFunc func(ctx context.Context) config.SetupRequirements

// StaticSetupRequirements always requires exactly the secrets keys.
func StaticSetupRequirements(keys ...string) SetupRequirementsFunc {
	return func(context.Context) config.SetupRequirements { return config.SetupRequirements{SecretKeys: keys} }
}

// SetupGuard returns Gin middleware that redirects every request to
// SetupPath while any of the required secrets keys has no stored value or
// the required 秘密鍵 path (立花) is unset (issues #80, #734; FR-SETUP-1). The
// redirect form depends on the client, because a 302 that
// a script-driven request silently follows would hand it the whole `/setup`
// HTML document (issue #140):
//   - `HX-Request: true` (HTMX fragment): 204 + `HX-Redirect: /setup`, so
//     htmx navigates the browser instead of swapping a page into a target.
//   - WebSocket upgrade: 403 (a redirect cannot complete the handshake).
//   - `/api/v1/...`: 503 JSON `{"setup_required":true,"setup_url":"/setup"}`
//     rather than HTML that `response.json()` cannot parse.
//   - Anything else (page navigation): 302 to SetupPath.
//
// Once everything required is stored, requests pass through from the very
// next one: the check reads the store on each request, so
// there is no cached "setup finished" state to invalidate.
//
// Exempt from the redirect, because the Setup screen cannot work without
// them: SetupPath itself, the static assets it links (`/static/...`) and
// the per-key save/delete routes it posts to (`POST`/`DELETE
// /settings/:key`, issue #79) and the broker selection/立花 settings rows
// (`POST`/`DELETE /ops-settings/:key` for config.IsBrokerSettingKey keys,
// issue #734), since choosing the broker is part of setting it up. Every other route - including `GET
// /settings`, `/api/v1/...` and WebSocket upgrades - is answered as above.
//
// A store read error counts as "unset" (the same degradation
// SettingsHandler's secrets-status banner and rows apply, issue #70): the
// operator lands on the Setup screen, which can overwrite an unreadable
// value, instead of the app trusting a key it cannot read.
func SetupGuard(store SecretsReader, required SetupRequirementsFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if setupGuardExempt(c.Request.Method, c.Request.URL.Path) || setupSatisfied(c.Request.Context(), store, required(c.Request.Context())) {
			c.Next()
			return
		}
		respondSetupRequired(c)
	}
}

func respondSetupRequired(c *gin.Context) {
	path := c.Request.URL.Path
	switch {
	case c.GetHeader("HX-Request") == "true":
		c.Header("HX-Redirect", SetupPath)
		c.AbortWithStatus(http.StatusNoContent)
	case strings.EqualFold(c.GetHeader("Upgrade"), "websocket"):
		c.AbortWithStatus(http.StatusForbidden)
	case path == "/api/v1" || strings.HasPrefix(path, "/api/v1/"):
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"setup_required": true, "setup_url": SetupPath})
	default:
		c.Redirect(http.StatusFound, SetupPath)
		c.Abort()
	}
}

func setupGuardExempt(method, path string) bool {
	switch {
	case path == SetupPath:
		return true
	case path == "/static" || strings.HasPrefix(path, "/static/"):
		return true
	case strings.HasPrefix(path, "/settings/"):
		return method == http.MethodPost || method == http.MethodDelete
	case strings.HasPrefix(path, "/ops-settings/"):
		return (method == http.MethodPost || method == http.MethodDelete) && config.IsBrokerSettingKey(strings.TrimPrefix(path, "/ops-settings/"))
	}
	return false
}

func setupSatisfied(ctx context.Context, store SecretsReader, required config.SetupRequirements) bool {
	if required.PrivateKeyPathKey != "" && !required.PrivateKeyPathSet {
		return false
	}
	for _, key := range required.SecretKeys {
		_, ok, err := store.Get(ctx, key)
		if err != nil {
			slog.Error("middleware: read stored secret for setup guard; treating as unset", "key", key, "error", err)
			return false
		}
		if !ok {
			return false
		}
	}
	return true
}
