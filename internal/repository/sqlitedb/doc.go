// Package sqlitedb opens the application's SQLite database (Open), applies
// the embedded golang-migrate migrations, writes consistent online backups
// (BackupTo), and instruments the driver (sqlmw) with slow-query logging and
// the DBWriteFailures streak that feeds the DB-write failure kill switch.
//
// It is a leaf: it depends only on internal/domain, the embedded migrations
// (the top-level db package) and external libraries. The resource-group packages never import it from
// production code; the composition root (bootstrap, cmd) opens the *sql.DB
// and hands it to each repository constructor. See
// docs/architecture/overview.md §3.
package sqlitedb
