// Package market persists market reference data: instruments
// (InstrumentRepository, instrument_repo.go) and market_snapshots
// (SnapshotRepository, snapshot_repo.go).
//
// It MUST depend only on internal/domain, internal/repository/sqlutil and
// the snapshotcols column table. See docs/architecture/overview.md §3.
package market
