// Package scanner serves the Scanner page (GET /scanner), the scan funnel
// (GET /scanner/scan and its export), the universe import
// (POST /scanner/universe/import), GET /api/v1/scanner* and /ws/scanner.
// It reads candidates through the CandidateSource port and depends only on
// internal/service, internal/domain, the universe importer wiring and
// handler/shared; it MUST NOT import sibling handler subpackages.
package scanner
