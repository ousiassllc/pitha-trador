// Package swagger serves the Swagger UI page (GET /swagger) that renders
// the Huma-generated OpenAPI spec at /api/v1/openapi.json with the vendored
// Stoplight Elements. The router registers it only when Swagger is enabled.
// It has no service dependency and MUST NOT import sibling handler subpackages.
package swagger
