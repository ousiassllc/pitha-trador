package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// SetupPath is the first-run Setup screen (`GET /setup`) the Setup Guard
// redirects to.
const SetupPath = "/setup"

// SecretsReader is the subset of internal/web/handler.SecretsStore the
// Setup Guard needs. An interface here keeps internal/web/middleware from
// depending on internal/repository directly (docs/architecture/overview.md
// §3 layer rule).
type SecretsReader interface {
	Get(ctx context.Context, key string) (plaintext string, ok bool, err error)
}

// SetupGuard returns Gin middleware that answers every request with a 302
// to SetupPath while any of requiredKeys has no stored value (issue #80,
// FR-SETUP-1). Once every required key is stored, requests pass through
// from the very next one: the check reads the store on each request, so
// there is no cached "setup finished" state to invalidate.
//
// Exempt from the redirect, because the Setup screen cannot work without
// them: SetupPath itself, the static assets it links (`/static/...`) and
// the per-key save/delete routes it posts to (`POST`/`DELETE
// /settings/:key`, issue #79). Every other route - including `GET
// /settings`, `/api/v1/...` and WebSocket upgrades - is redirected.
//
// A store read error counts as "unset" (the same degradation
// SettingsHandler's secrets-status banner and rows apply, issue #70): the
// operator lands on the Setup screen, which can overwrite an unreadable
// value, instead of the app trusting a key it cannot read.
func SetupGuard(store SecretsReader, requiredKeys []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if setupGuardExempt(c.Request.Method, c.Request.URL.Path) || requiredKeysConfigured(c.Request.Context(), store, requiredKeys) {
			c.Next()
			return
		}
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
	}
	return false
}

func requiredKeysConfigured(ctx context.Context, store SecretsReader, requiredKeys []string) bool {
	for _, key := range requiredKeys {
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
