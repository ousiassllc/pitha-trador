package swagger

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// swaggerHTML embeds Stoplight Elements (docs/environment/setup.md
// "Swagger / OpenAPI") pointed at the Huma-generated OpenAPI 3.1 spec.
// Elements is vendored from the bun-installed `@stoplight/elements`
// package (static/esbuild.config.mjs copies it to dist/vendor/) and served
// same-origin from the embedded /static/dist/vendor/stoplight-elements/, so
// no third-party CDN script runs on the origin that exposes the Kill Switch
// and settings APIs (issues #112/#117). The spec endpoint
// (/api/v1/openapi.json) is registered by the Huma routes.
const swaggerHTML = `<!DOCTYPE html>
<html lang="ja">
<head>
  <meta charset="utf-8">
  <title>pitha-trador API Docs</title>
  <script src="/static/dist/vendor/stoplight-elements/web-components.min.js"></script>
  <link rel="stylesheet" href="/static/dist/vendor/stoplight-elements/styles.min.css">
  <style>html, body { height: 100%; margin: 0; }</style>
</head>
<body>
  <elements-api
    apiDescriptionUrl="/api/v1/openapi.json"
    router="hash"
    layout="sidebar"
  ></elements-api>
</body>
</html>
`

// SwaggerUI serves the Stoplight Elements static HTML page for the
// `/swagger` route. It is intentionally a single static page (no dynamic
// data, no dependency on service/domain): Elements itself fetches
// apiDescriptionUrl client-side. Elements injects inline styles at runtime,
// so the page replaces the global CSP (middleware.SecurityHeaders) with
// the style-relaxed middleware.SwaggerCSP.
func SwaggerUI(c *gin.Context) {
	c.Header("Content-Security-Policy", middleware.SwaggerCSP)
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerHTML))
}
