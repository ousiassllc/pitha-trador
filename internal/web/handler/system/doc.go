// Package system serves the system state and controls: GET /system/status,
// /ws/system, the Kill Switch API (POST /api/v1/system/{pause,resume,kill}),
// the update UI (/system/update-*), /system/marketdata-status and
// GET /api/v1/logs/errors.
// It depends on internal/service, internal/domain, internal/logging,
// internal/version, web/middleware and handler/shared (plus Templ under
// internal/web); it MUST NOT import sibling handler subpackages.
package system
