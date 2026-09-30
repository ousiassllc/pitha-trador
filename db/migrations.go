// Package db embeds the golang-migrate SQL migrations applied to the
// application's SQLite database at startup (docs/architecture/overview.md
// §3, §10.1). Embedding the migrations directory keeps every migration file
// inside the single Wails executable instead of requiring `db/migrations`
// to be shipped alongside it as a separate directory on disk.
//
// Migration files follow golang-migrate's `{version}_{title}.up.sql` /
// `.down.sql` naming convention
// (https://github.com/golang-migrate/migrate#migration-sequences). Later
// sub-scopes append new numbered up/down pairs under migrations/; existing
// files MUST NOT be edited or renumbered once merged, since golang-migrate
// tracks the applied version per database file.
package db

import "embed"

// MigrationsFS holds every SQL migration file under migrations/, embedded
// at build time so internal/repository/sqlitedb.Open can apply them via
// golang-migrate without depending on the filesystem layout at runtime.
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS
