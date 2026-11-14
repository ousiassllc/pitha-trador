// Package migratebundle builds and reads the PC migration bundle (issue
// #1075, FR-MIGRATE-*): ONE file that carries the application's data and
// settings from an old PC to a new one.
//
// The bundle is a ZIP archive (extension Extension) holding
//
//	manifest.json   what the file is: bundle format version, the DB schema
//	                (golang-migrate) version, the app version, the creation
//	                time, the SHA-256 of the database, per-table row counts
//	                and the runtime settings left out
//	pitha.db        a consistent, scrubbed copy of the SQLite database
//
// There is deliberately no separate settings file: the non-secret settings
// are the database's runtime_settings table, so one file has one source of
// truth and the restore is exactly "swap the database".
//
// Secrets never go into the file: the secrets table is emptied
// (backup.CopyScrubbed, the daily backup's scrub) and so are the
// runtime_settings rows that are machine-specific or secret-bearing
// (ExcludedSettingKeys and any row logging's masking rules would change).
// The copy is VACUUMed with secure_delete, so the removed values are not in
// the file's free pages either. A bundle that carries any of it is refused on
// import.
//
// This package knows the file format (Export, Extract). Staging and applying
// an import - which replaces the live database - is internal/service/
// migratebundle/importflow.
package migratebundle
