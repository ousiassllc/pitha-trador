package jev

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// ObservationGate is what Scout needs to run observation-only symbols (issue
// #1055): symbols that are watched and asked on Jev Scout to collect
// evaluation data but are never traded. *observation.Gate implements it.
type ObservationGate interface {
	// Observed reports whether symbol is an observation-only symbol now.
	Observed(ctx context.Context, symbol string) (bool, error)
	// Admit reports whether one more observation call may be made now. When it
	// may not, reason says why (a cost cap or failure cap of the day).
	Admit(ctx context.Context) (ok bool, reason string, err error)
	// RecordCall counts a completed observation call (resp is Jev's answer) and
	// RecordFailure a failed one, against the day's caps.
	RecordCall(ctx context.Context, resp ScoutResponse) error
	RecordFailure(ctx context.Context) error
}

// ObservationCapSkip is the note of a jev-scout job skipped because the
// observation daily cap was reached (jobqueue.Skip).
const ObservationCapSkip = "observation daily cap reached; no Jev call was made"

func (s *Scout) isObserved(ctx context.Context, symbol string) (bool, error) {
	if s.observation == nil {
		return false, nil
	}
	observed, err := s.observation.Observed(ctx, symbol)
	if err != nil {
		// Unknown standing: do neither (a retry decides), rather than treat an
		// observation symbol as a tradable one.
		return false, fmt.Errorf("jev: read the observation symbols for %q: %w", symbol, err)
	}
	return observed, nil
}

// handleObservation runs one jev-scout job of an observation-only symbol: the
// Scout call itself (the evaluation data is the point) under the observation
// daily cap, and nothing after it. The jev-trader job is never enqueued, so no
// observation symbol reaches Policy, an order, Paper or Live. A day's cap
// stops these calls only; Scout and Trader of every other symbol are not gated.
func (s *Scout) handleObservation(ctx context.Context, payload ScoutJobPayload, snap domain.Snapshot) error {
	ok, reason, err := s.observation.Admit(ctx)
	if err != nil {
		return fmt.Errorf("jev: check the observation cap for %q: %w", payload.Symbol, err)
	}
	if !ok {
		slog.Info("jev: skip observation scout, daily cap reached", "symbol", payload.Symbol, "reason", reason)
		return jobqueue.Skip(ObservationCapSkip)
	}
	// Not reported to the ScoutRecorder: the Scanner Dashboard's Scout stage
	// counts the candidates that go on to Trader, which these never do.
	_, resp, _, err := s.evaluate(ctx, payload.InstrumentID, StateFromSnapshot(snap, s.now().UTC()))
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
