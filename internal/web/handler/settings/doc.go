// Package settings serves the /setup and /settings pages, credential
// save/delete (POST/DELETE /settings/:key) and GET /system/secrets-status,
// backed by the SecretsStore port.
// It depends on internal/config, internal/service, web/middleware and
// handler/shared (plus Templ under internal/web); it MUST NOT import sibling
// handler subpackages.
package settings
