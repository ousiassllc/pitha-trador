// Package system serves the system state and controls: GET /system/status,
// /ws/system, the Kill Switch API (POST /api/v1/system/{pause,resume,kill}),
// the update UI (/system/update-*), /system/marketdata-status and
// GET /api/v1/logs/errors.
// It depends only on internal/service, internal/domain and handler/shared;
// it MUST NOT import sibling handler subpackages.
package system
