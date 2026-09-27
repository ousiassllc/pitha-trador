// Package domain contains the project's domain models (instruments, market
// snapshots, features, Jev decisions, signals, orders, positions,
// calibration results, policy proposals, etc).
//
// This package MUST NOT depend on any other internal package
// (repository/service/router/web). See docs/architecture/overview.md §3 for
// the layer dependency rules (handler → service → repository → domain).
//
// Instrument, Snapshot, Feature, Candidate and JevDecision are introduced
// by earlier sub-scopes and this one (instrument.go, snapshot.go,
// feature.go, candidate.go, jevdecision.go). The remaining concrete
// domain types (signals, orders, positions, calibration results, policy
// proposals) are introduced by later sub-scopes.
package domain
