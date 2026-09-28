// Package config loads the YAML configuration files under config/
// (strategy.yaml, risk.yaml) into typed Go structs.
//
// This package MUST NOT depend on any other internal package
// (domain/repository/service/router/web); it only reads and parses files
// from disk. See docs/architecture/overview.md §3 for the config/ role
// (スキャン頻度・Fast Screenerしきい値・Policy Engineしきい値／Risk Engine
// 制限値) and §1「AI自己改善ループの境界」for why risk.yaml is not writable
// by the self-improvement loop.
package config
