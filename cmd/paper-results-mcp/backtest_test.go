package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

func TestBacktestSources_OpensReadOnlyOnFirstUseAndRejectsBadInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pitha.db")
	conn, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	b := &backtestSources{dbPath: path}
	if b.state != nil {
		t.Fatal("the database was opened before the first backtest")
	}
	if _, err := b.Source(ctx, "shadow"); err == nil {
		t.Error("an unknown replay mode was accepted")
	}
	if b.state != nil {
		t.Error("a bad replay mode opened the database")
	}
	for _, mode := range []string{"paper", "live"} {
		src, err := b.Source(ctx, mode)
		if err != nil {
			t.Fatalf("Source(%q): %v", mode, err)
		}
		// An empty database has no instruments: an empty replay input, not an error.
		now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		cfgs, err := src.RunConfigs(ctx, backtest.Period{Start: now, End: now.Add(24 * time.Hour)})
		if err != nil || len(cfgs) != 0 {
			t.Fatalf("RunConfigs(%q) = %d configs, %v", mode, len(cfgs), err)
		}
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("a second Close: %v", err)
	}
}

func TestBacktestSources_AMissingDatabaseIsAnErrorAndNotCached(t *testing.T) {
	b := &backtestSources{dbPath: filepath.Join(t.TempDir(), "absent.db")}
	if _, err := b.Source(context.Background(), "paper"); err == nil {
		t.Fatal("a missing database was opened")
	}
	if b.state != nil {
		t.Fatal("a failed open was cached")
	}
}
