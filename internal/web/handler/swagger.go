package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// swaggerHTML embeds Stoplight Elements (docs/environment/setup.md
// "Swagger / OpenAPI") pointed at the Huma-generated OpenAPI 3.1 spec.
// Elements is loaded from a CDN so no build-time frontend dependency is
// required; the spec endpoint (/api/v1/openapi.json) is registered by a
// later Huma sub-scope and may 404 until then, but this page itself still
// renders.
const swaggerHTML = `<!DOCTYPE html>
<html lang="ja">
<head>
  <meta charset="utf-8">
  <title>pitha-trador API Docs</title>
  <script src="https://unpkg.com/@stoplight/elements/web-components.min.js"></script>
  <link rel="stylesheet" href="https://unpkg.com/@stoplight/elements/styles.min.css">
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
// apiDescriptionUrl client-side.
func SwaggerUI(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerHTML))
}
