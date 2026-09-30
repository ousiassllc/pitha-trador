// Package sqlutil holds the helpers shared by the repository resource-group
// packages: the SQLite representation of time.Time (FormatTime/ParseTime),
// the Nullable*/Null* adapters that map nil pointers to SQL NULL and back,
// and the RowScanner/Execer/Executor interfaces that let scan and insert
// helpers run over *sql.Row/*sql.Rows and *sql.DB/*sql.Tx alike.
//
// It is a leaf: it depends only on the standard library. See
// docs/architecture/overview.md §3 for the repository layer rules.
package sqlutil
