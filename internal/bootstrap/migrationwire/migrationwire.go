// Package migrationwire wires the PC migration bundle (issue #1075,
// FR-MIGRATE-*) into the router: the exporter over the live database and the
// import flow that stages an import for the next start. The start-up half -
// applying a staged import before the database is opened - is
// startup.OpenDatabase.
package migrationwire

import (
	"database/sql"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/importflow"
)

// Option is router.WithMigration over db (the live database, whose file is
// dbPath). Entrypoints pass it next to bootstrap.RouterOptions.
func Option(db *sql.DB, dbPath string) router.Option {
	return router.WithMigration(migratebundle.NewExporter(db), importflow.New(db, dbPath))
}
