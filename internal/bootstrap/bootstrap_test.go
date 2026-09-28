package bootstrap_test

import (
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
)

func TestRun_OpensDBAndLoadsConfigFromRepoDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pitha.db")

	state, err := bootstrap.Run(bootstrap.Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })

	if state.DB == nil {
		t.Fatal("Run: State.DB is nil")
	}
	if err := state.DB.Ping(); err != nil {
		t.Fatalf("State.DB.Ping: %v", err)
	}
	if state.Strategy == nil {
		t.Fatal("Run: State.Strategy is nil")
	}
	if state.Strategy.FastScreener.TopN == 0 {
		t.Error("Run: State.Strategy.FastScreener.TopN was not loaded from config/strategy.yaml")
	}
	if state.Risk == nil {
		t.Fatal("Run: State.Risk is nil")
	}
	if state.Risk.Paper.MaxOpenPositions == 0 {
		t.Error("Run: State.Risk.Paper.MaxOpenPositions was not loaded from config/risk.yaml")
	}
	if state.Paths.DBPath != dbPath {
		t.Errorf("Run: Paths.DBPath = %q, want %q", state.Paths.DBPath, dbPath)
	}
}

func TestRun_EnvOverridesTakePrecedenceOverDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "override.db")
	t.Setenv(bootstrap.EnvDBPath, dbPath)

	state, err := bootstrap.Run(bootstrap.Config{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })

	if state.Paths.DBPath != dbPath {
		t.Errorf("Run: Paths.DBPath = %q, want env override %q", state.Paths.DBPath, dbPath)
	}
}

func TestRun_ReturnsErrorAndClosesDBOnUnparsableStrategyConfig(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pitha.db")

	_, err := bootstrap.Run(bootstrap.Config{
		DBPath:       dbPath,
		StrategyPath: filepath.Join(t.TempDir(), "does-not-exist.yaml"),
	})
	if err == nil {
		t.Fatal("Run: expected an error for a missing strategy config file, got nil")
	}
}

func TestDefaultDBPath_ReturnsPithaTradorSubpath(t *testing.T) {
	path, err := bootstrap.DefaultDBPath()
	if err != nil {
		t.Fatalf("DefaultDBPath: %v", err)
	}
	if filepath.Base(path) != "pitha.db" {
		t.Errorf("DefaultDBPath: base = %q, want %q", filepath.Base(path), "pitha.db")
	}
	if filepath.Base(filepath.Dir(path)) != "pitha-trador" {
		t.Errorf("DefaultDBPath: parent dir = %q, want %q", filepath.Base(filepath.Dir(path)), "pitha-trador")
	}
}
