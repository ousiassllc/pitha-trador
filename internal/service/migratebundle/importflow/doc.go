// Package importflow is the import half of the PC migration bundle (issue
// #1075, FR-MIGRATE-*). The running application holds its SQLite database
// open (WAL, pooled connections), so an import never replaces it live. It is
// two-phase:
//
//  1. Service.Preview (upload) validates the bundle - format and schema
//     compatibility first, then size/SHA-256, integrity_check, manifest
//     counts and "no secrets" - stages the verified database under
//     <db dir>/migration/staging/<token>/ and returns what would be replaced
//     (per-table row counts of the current database vs the bundle). Nothing
//     in the live data changes. Service.Confirm (explicit, and
//     replaceExisting when the current database has user data) moves the
//     staged database into <db dir>/migration/pending/ with a marker.
//  2. ApplyPending runs at the next start, before the database is opened:
//     it takes a safety copy of the current database
//     (<db dir>/migration/safety/, VACUUM INTO), carries this PC's own
//     secrets and machine-specific settings over into the staged database,
//     migrates it to the running schema and atomically renames it into
//     place. Any failure leaves the current database as it was and is
//     recorded in <db dir>/migration/last-result.json (Service.LastResult).
//
// Cancel removes whatever is staged or pending.
package importflow
