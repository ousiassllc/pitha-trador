// Package calibration serves the Calibration page (GET /calibration) and
// GET /api/v1/calibration, backed by the CalibrationSource port.
// It depends only on internal/domain and handler/shared (plus Templ under
// internal/web); it MUST NOT import sibling handler subpackages.
package calibration
