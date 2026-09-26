// Package domain contains the project's domain models (instruments, market
// snapshots, features, Jev decisions, signals, orders, positions,
// calibration results, policy proposals, etc).
//
// This package MUST NOT depend on any other internal package
// (repository/service/router/web). See docs/architecture/overview.md §3 for
// the layer dependency rules (handler → service → repository → domain).
//
// Instrument, Snapshot and Feature are introduced by this sub-scope
// (instrument.go, snapshot.go, feature.go). The remaining concrete domain
// types (Jev decisions, signals, orders, positions, calibration results,
// policy proposals) are introduced by later sub-scopes.
package domain
