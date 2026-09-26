// Package repository provides persistence access (SQLite via database/sql +
// sqlc-generated queries) for the domain models.
//
// This package MUST depend only on internal/domain. It MUST NOT depend on
// internal/service, internal/router or internal/web. See
// docs/architecture/overview.md §3 for the layer dependency rules
// (handler → service → repository → domain).
//
// Concrete repositories (instrument_repo.go, snapshot_repo.go, ...) are
// introduced by later sub-scopes (DB接続・マイグレーション基盤 and beyond);
// this file only establishes the package skeleton.
package repository
