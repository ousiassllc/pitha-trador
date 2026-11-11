package jev

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// ObservationGate is what Scout needs to run observation-only symbols (issue
// #1055): watched and asked on Jev Scout for evaluation data, never traded.
type ObservationGate interface {
	// Observed reports whether symbol is observation-only now (never a held one).
	// An error means the standing is unknown.
	Observed(ctx context.Context, symbol string) (bool, error)
	// Admit reports whether one more observation call may be made now (else why).
	Admit(ctx context.Context) (ok bool, reason string, err error)
	// RecordCall counts a completed observation call (resp is Jev's answer) and
	// RecordFailure a failed one, against the day's caps.
	RecordCall(ctx context.Context, resp ScoutResponse) error
	RecordFailure(ctx context.Context) error
}

// Notes of a jev-scout job skipped (jobqueue.Skip): the observation daily cap was
// reached / the symbol's observation standing could not be read.
const (
	ObservationCapSkip     = "observation daily cap reached; no Jev call was made"
	ObservationUnknownSkip = "observation standing unknown; skipped this round (no Jev call, no entry)"
)

func (s *Scout) isObserved(ctx context.Context, symbol string) (bool, error) {
	if s.observation == nil {
		return false, nil
	}
	return s.observation.Observed(ctx, symbol)
}

// handleObservation runs one jev-scout job of an observation-only symbol: the
// Scout call under the observation daily cap and nothing after it (no jev-trader
// job, so no Policy, order, Paper or Live). The cap stops these calls only.
func (s *Scout) handleObservation(ctx context.Context, payload ScoutJobPayload, snap domain.Snapshot) error {
	ok, reason, err := s.observation.Admit(ctx)
	if err != nil {
		return fmt.Errorf("jev: check the observation cap for %q: %w", payload.Symbol, err)
	}
	if !ok {
		slog.Info("jev: skip observation scout, daily cap reached", "symbol", payload.Symbol, "reason", reason)
		return jobqueue.Skip(ObservationCapSkip)
	}
	// Not reported to the ScoutRecorder: its Scout stage counts candidates that reach Trader.
	_, resp, _, err := s.evaluate(ctx, payload.InstrumentID, StateFromSnapshot(snap, s.now().UTC()), false)
	if err != nil {
		if recErr := s.observation.RecordFailure(ctx); recErr != nil {
			slog.Error("jev: record a failed observation call", "symbol", payload.Symbol, "error", recErr)
		}
		return err
	}
	if recErr := s.observation.RecordCall(ctx, resp); recErr != nil {
		slog.Error("jev: record an observation call", "symbol", payload.Symbol, "error", recErr)
	}
	return nil
}
