// Package router builds the single Gin engine shared by cmd/desktop (via
// Wails' options.App.AssetServer.Handler) and cmd/server (via net/http). It
// intentionally has no dependency on Wails so that a headless server can be
// run without a Wails process (see docs/architecture/overview.md §9).
package router

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

const placeholderHTML = `<!DOCTYPE html>
<html lang="ja">
<head>
  <meta charset="utf-8">
  <title>pitha-trador</title>
</head>
<body>
  <h1>pitha-trador</h1>
  <p>Backend skeleton is running.</p>
</body>
</html>
`

// New builds and returns the shared Gin engine.
//
// Route registration is intentionally minimal at this stage: only a
// placeholder page is served so callers (Wails desktop shell / cmd/server)
// have something to render end-to-end. SSR routes (Templ/HTMX, registered
// via internal/web/handler) and the `/api/v1` Huma-based JSON API are
// registered by later sub-scopes on top of this engine; this function is the
// receiving point ("受け皿") for that future registration.
func New() *gin.Engine {
	engine := gin.New()
	engine.GET("/", handlePlaceholder)
	if swaggerEnabled() {
		engine.GET("/swagger", handler.SwaggerUI)
	}
	return engine
}

// swaggerEnabled reports whether the `/swagger` route (Stoplight Elements
// UI, docs/environment/setup.md "Swagger / OpenAPI") should be registered.
// Controlled by the SWAGGER_ENABLED env var: defaults to true (dev/staging)
// and is disabled only when explicitly set to "false" (production, Phase 7
// live trading).
func swaggerEnabled() bool {
	return os.Getenv("SWAGGER_ENABLED") != "false"
}

func handlePlaceholder(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(placeholderHTML))
}
