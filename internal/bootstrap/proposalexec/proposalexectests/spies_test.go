package proposalexectests

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/proposalexec"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// spyExecution counts the calls that place an order, so a test can show that a
// proposal Risk rejected never reached Enter. Two hooks let a test stage a race:
// beforeClaim runs once at the processor's first look at the clock of a
// proposal (after the proposal was read, before it is claimed), and
// sessionOpen, when set, is what EntrySessionOpenNow answers (Enter itself still
// judges the real clock).
type spyExecution struct {
	*execution.Engine
	enters      int
	closes      int
	beforeClaim func()
	sessionOpen *bool
}

func (s *spyExecution) Enter(ctx context.Context, req execution.EntryRequest) (execution.EntryResult, error) {
	s.enters++
	return s.Engine.Enter(ctx, req)
}

func (s *spyExecution) CloseAtMarket(ctx context.Context, positionID int64, reason string, now time.Time) (domain.Position, error) {
	s.closes++
	return s.Engine.CloseAtMarket(ctx, positionID, reason, now)
}

// spyRisk counts CheckThenEnter calls, so a test can show that a proposal
// rejected before Risk never reached it.
type spyRisk struct {
	proposalexec.Risk
	checks int
}

func (s *spyRisk) CheckThenEnter(ctx context.Context, instrumentID int64, direction string, enter func(size risk.EntrySize) error) (bool, string, error) {
	s.checks++
	return s.Risk.CheckThenEnter(ctx, instrumentID, direction, enter)
}

func (s *spyExecution) Config() execution.Config {
	if hook := s.beforeClaim; hook != nil {
		s.beforeClaim = nil
		hook()
	}
	return s.Engine.Config()
}

func (s *spyExecution) EntrySessionOpenNow() bool {
	if s.sessionOpen != nil {
		return *s.sessionOpen
	}
	return s.Engine.EntrySessionOpenNow()
}
