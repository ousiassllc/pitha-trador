// Package wiringflow_test holds black-box tests of bootstrap.BuildServices's
// composition: each one drives the exported *bootstrap.Services (repositories,
// Scheduler, Risk, Policy, ...) the way cmd/desktop does. Kept out of
// internal/bootstrap so that directory stays under linterly's line limit (#1115).
package wiringflow_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// tradingHours is a fixed instant inside the 前場 (2026-09-29 10:00 JST):
// paper entries are session-gated, so tests must not enter at time.Now().
var tradingHours = time.Date(2026, 9, 29, 10, 0, 0, 0, marketcalendar.JST)

// newTestServices builds a *Services backed by a fresh temp-dir SQLite DB.
func newTestServices(t *testing.T) *bootstrap.Services {
	svc, _ := newTestServicesWithState(t)
	return svc
}

func newTestServicesWithState(t *testing.T) (*bootstrap.Services, *bootstrap.State) {
	t.Helper()
	state, err := bootstrap.Run(bootstrap.Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	// These tests size and fill orders of ordinary-priced stocks (2,500 yen)
	// and are about the wiring, not the Paper capital: give them the roomy
	// pre-#804 Paper limits instead of the shipped 500,000 yen ones, under which
	// max_trade_loss_pct skips any stock above about 2,083 yen.
	state.Risk.Paper.InitialCapital, state.Risk.Paper.MaxPositionPerSymbolPct, state.Risk.Paper.MaxTotalExposurePct = 30_000_000, 2, 20
	// Jev calls fail after one attempt (WithJevMaxAttempts below): tests here
	// only need a Handler to run, not the real 1.5s retry backoff (issue #236).
	// JevBaseURL points at a closed local port: an empty one would fall back
	// to the production host (issue #271) and make these tests dial it.
	return bootstrap.BuildServices(state, config.Secrets{KabuAPIPassword: "test-password", JevBaseURL: "http://127.0.0.1:1"},
		bootstrap.WithJevMaxAttempts(1), bootstrap.WithExecutionClock(func() time.Time { return tradingHours.Add(time.Minute) })), state
}

func mustCreateInstrument(t *testing.T, svc *bootstrap.Services, symbol string) domain.Instrument {
	t.Helper()
	inst, err := svc.Instruments.Create(context.Background(), domain.Instrument{
		Symbol: symbol, Name: symbol + " Inc.", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create instrument %q: %v", symbol, err)
	}
	return inst
}
