// Package router builds the single Gin engine shared by cmd/desktop (via
// Wails' options.App.AssetServer.Handler) and cmd/server (via net/http). It
// intentionally has no dependency on Wails so that a headless server can be
// run without a Wails process (see docs/architecture/overview.md §9).
package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
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
	return engine
}

func handlePlaceholder(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(placeholderHTML))
}
